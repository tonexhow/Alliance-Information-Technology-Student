package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"golang.org/x/crypto/bcrypt"
)

type memberSessionEntry struct {
	AccountID       int64
	Name            string
	Username        string
	Email           string
	Avatar          string
	Role            string
	RoleName        string
	DivisionID      int64
	Division        string
	MembershipLevel string
	IsAdmin         bool
	ExpiresAt       time.Time
}

func (a *appServer) ensureBootstrapDeveloper(ctx context.Context, username, displayName, password string) error {
	if a.db == nil {
		return nil
	}
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	password = strings.TrimSpace(password)
	if username == "" {
		username = "tonybcc"
	}
	if displayName == "" {
		displayName = "Tony BCC"
	}

	// Never create a predictable developer password implicitly. Production only
	// bootstraps this account when an explicit developer password is configured.
	if password == "" {
		return nil
	}

	var existingAccountID int64
	err := a.db.Pool().QueryRow(ctx, `
        SELECT a.id
        FROM accounts a
        JOIN account_roles ar ON ar.account_id=a.id AND ar.is_active=TRUE
        JOIN roles r ON r.id=ar.role_id AND r.code='developer'
        WHERE LOWER(a.username)=LOWER($1) AND a.status='active'
        LIMIT 1`, username).Scan(&existingAccountID)
	if err == nil {
		return nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var accountID int64
	err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE LOWER(username)=LOWER($1) LIMIT 1`, username).Scan(&accountID)
	if err != nil {
		if err != pgx.ErrNoRows {
			return err
		}
		err = tx.QueryRow(ctx, `
            INSERT INTO accounts (username, email, password_hash, display_name, account_kind, status)
            VALUES ($1, NULL, $2, $3, 'member', 'active')
            RETURNING id`, username, string(hash), displayName).Scan(&accountID)
		if err != nil {
			return fmt.Errorf("seed developer account: %w", err)
		}
	} else {
		if _, err = tx.Exec(ctx, `
            UPDATE accounts
            SET password_hash=$1, display_name=COALESCE(NULLIF(display_name,''),$2), account_kind='member', status='active', updated_at=NOW()
            WHERE id=$3`, string(hash), displayName, accountID); err != nil {
			return err
		}
	}

	var divisionID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM divisions WHERE code='codehex'`).Scan(&divisionID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
        INSERT INTO member_profiles (account_id, member_number, department, course, year_level, primary_division_id, membership_level, joined_at)
        VALUES ($1, 'DEV-0001', 'College of Information Technology Education', 'Bachelor of Science in Information Technology', '4th Year', $2, 'Core Member', CURRENT_DATE)
        ON CONFLICT (account_id) DO UPDATE SET
          primary_division_id=COALESCE(member_profiles.primary_division_id, EXCLUDED.primary_division_id),
          membership_level=CASE WHEN member_profiles.membership_level='' THEN EXCLUDED.membership_level ELSE member_profiles.membership_level END,
          updated_at=NOW()`, accountID, divisionID)
	if err != nil {
		return err
	}

	var roleID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM roles WHERE code='developer'`).Scan(&roleID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
        INSERT INTO account_roles (account_id, role_id, division_id, is_active)
        VALUES ($1,$2,NULL,TRUE)
        ON CONFLICT DO NOTHING`, accountID, roleID)
	if err != nil {
		return err
	}

	var notificationCount int
	_ = tx.QueryRow(ctx, `SELECT COUNT(*) FROM notifications WHERE account_id=$1`, accountID).Scan(&notificationCount)
	if notificationCount == 0 {
		_, err = tx.Exec(ctx, `INSERT INTO notifications (account_id, notification_type, title, body) VALUES
          ($1,'system','Your member profile is ready','Your AITS member profile is active and ready to use.'),
          ($1,'post','Welcome to the AITS community feed','Follow public, organization, and division updates from your member space.'),
          ($1,'security','Developer access enabled','Your developer account can open the member area first, then enter Administration.')`, accountID)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (a *appServer) ensureDevelopmentTestAccounts(ctx context.Context) error {
	if a.db == nil {
		return nil
	}

	seeds := []struct {
		Username       string
		Name           string
		Role           string
		RoleName       string
		HierarchyLevel int
		Scope          string
		IsAdmin        bool
	}{
		{"aitsinstructor", "AITS Adviser", "instructor", "Adviser", 90, "global", true},
		{"aitspresident", "AITS President", "president", "President", 80, "global", true},
		{"aitsvicepresident", "AITS Vice President", "vice_president", "Vice President", 70, "global", true},
		{"aitssecretary", "AITS Secretary", "secretary", "Secretary", 60, "global", true},
		{"aitstreasurer", "AITS Treasurer", "treasurer", "Treasurer", 60, "global", true},
		{"aitspio", "AITS Public Information Officer", "pio", "Public Information Officer", 60, "global", true},
		{"aitsmayor", "AITS Division Mayor", "mayor", "Division Mayor", 50, "division", true},
		{"aitsmember", "AITS Member", "member", "Member", 10, "global", false},
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("1234567890"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash development password: %w", err)
	}

	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin development account seed: %w", err)
	}
	defer tx.Rollback(ctx)

	// Development identities should never make the whole server depend on a
	// particular division already being active. Prefer an existing active
	// division; when a fresh/older database has none, restore the canonical
	// CodeHex row so the division-scoped mayor test account still has a scope.
	var divisionID int64
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM divisions
		WHERE is_active=TRUE
		ORDER BY sort_order, id
		LIMIT 1`).Scan(&divisionID)
	if err == pgx.ErrNoRows {
		err = tx.QueryRow(ctx, `
			INSERT INTO divisions (code, name, sort_order, is_active)
			VALUES ('codehex', 'CodeHex', 10, TRUE)
			ON CONFLICT (code) DO UPDATE
			SET name=EXCLUDED.name,
			    sort_order=EXCLUDED.sort_order,
			    is_active=TRUE,
			    updated_at=NOW()
			RETURNING id`).Scan(&divisionID)
	}
	if err != nil {
		return fmt.Errorf("prepare development test division: %w", err)
	}

	for _, seed := range seeds {
		// Older databases may be missing a newer role even when their schema
		// otherwise opens successfully. Ensure each development role exists,
		// but do not overwrite an existing role's configured metadata.
		var roleID int64
		var scope string
		err = tx.QueryRow(ctx, `
			INSERT INTO roles (code, name, hierarchy_level, scope, is_administrator)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (code) DO UPDATE SET code=EXCLUDED.code
			RETURNING id, scope`,
			seed.Role, seed.RoleName, seed.HierarchyLevel, seed.Scope, seed.IsAdmin,
		).Scan(&roleID, &scope)
		if err != nil {
			return fmt.Errorf("prepare %s role: %w", seed.Username, err)
		}

		var accountID int64
		err = tx.QueryRow(ctx, `
			SELECT id
			FROM accounts
			WHERE LOWER(username)=LOWER($1)
			LIMIT 1`, seed.Username).Scan(&accountID)
		if err != nil && err != pgx.ErrNoRows {
			return fmt.Errorf("find %s account: %w", seed.Username, err)
		}

		if err == pgx.ErrNoRows {
			err = tx.QueryRow(ctx, `
				INSERT INTO accounts
					(username,email,password_hash,display_name,account_kind,status)
				VALUES
					($1,NULL,$2,$3,'member','active')
				RETURNING id`,
				seed.Username, string(passwordHash), seed.Name,
			).Scan(&accountID)
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE accounts
				SET password_hash=$1,
				    display_name=$2,
				    account_kind='member',
				    status='active',
				    updated_at=NOW()
				WHERE id=$3`,
				string(passwordHash), seed.Name, accountID,
			)
		}
		if err != nil {
			return fmt.Errorf("seed %s account: %w", seed.Username, err)
		}

		// Tie the synthetic member number to the immutable account id. This
		// avoids collisions left by partially-created development accounts.
		memberNumber := fmt.Sprintf("DEVTEST-%06d", accountID)
		membership := "Member"
		if seed.Role != "member" {
			membership = "Core Member"
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO member_profiles
				(account_id,member_number,department,course,year_level,primary_division_id,membership_level,joined_at)
			VALUES
				($1,$2,'College of Information Technology Education','Bachelor of Science in Information Technology','4th Year',$3,$4,CURRENT_DATE)
			ON CONFLICT(account_id) DO UPDATE SET
				member_number=EXCLUDED.member_number,
				department=EXCLUDED.department,
				course=EXCLUDED.course,
				year_level=EXCLUDED.year_level,
				primary_division_id=EXCLUDED.primary_division_id,
				membership_level=EXCLUDED.membership_level,
				updated_at=NOW()`,
			accountID, memberNumber, divisionID, membership,
		)
		if err != nil {
			return fmt.Errorf("seed %s profile: %w", seed.Username, err)
		}

		if _, err = tx.Exec(ctx, `
			UPDATE account_roles
			SET is_active=FALSE, updated_at=NOW()
			WHERE account_id=$1`, accountID,
		); err != nil {
			return fmt.Errorf("reset %s access: %w", seed.Username, err)
		}

		var roleDivision any
		if scope == "division" {
			roleDivision = divisionID
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO account_roles
				(account_id,role_id,division_id,is_active)
			VALUES
				($1,$2,$3,TRUE)
			ON CONFLICT DO NOTHING`,
			accountID, roleID, roleDivision,
		)
		if err == nil {
			_, err = tx.Exec(ctx, `
				UPDATE account_roles
				SET is_active=TRUE,
				    starts_at=NOW(),
				    expires_at=NULL,
				    updated_at=NOW()
				WHERE account_id=$1
				  AND role_id=$2
				  AND COALESCE(division_id,0)=COALESCE($3::bigint,0)`,
				accountID, roleID, roleDivision,
			)
		}
		if err != nil {
			return fmt.Errorf("seed %s access: %w", seed.Username, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit development test accounts: %w", err)
	}
	return nil
}

func (a *appServer) memberLogin(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, 503, map[string]any{"ok": false, "error": "AITS member services are temporarily unavailable"})
		return
	}
	var req struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid request"})
		return
	}
	req.Identifier = strings.TrimSpace(req.Identifier)
	if req.Identifier == "" || req.Password == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "username and password are required"})
		return
	}
	if isDevelopmentTestUsername(req.Identifier) && !isLocalDevelopmentRequest(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "invalid member credentials"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var accountID int64
	var hash, status string
	err := a.db.Pool().QueryRow(ctx, `
        SELECT id, COALESCE(password_hash,''), status FROM accounts
        WHERE account_kind='member' AND (LOWER(username)=LOWER($1) OR LOWER(COALESCE(email,''))=LOWER($1))
        LIMIT 1`, req.Identifier).Scan(&accountID, &hash, &status)
	if err != nil || hash == "" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "invalid member credentials"})
		return
	}
	if status == "restricted" {
		var reason string
		var liftsAt *time.Time
		rerr := a.db.Pool().QueryRow(ctx, `SELECT reason,lifts_at FROM member_restrictions WHERE account_id=$1 AND lifted_at IS NULL ORDER BY restricted_at DESC LIMIT 1`, accountID).Scan(&reason, &liftsAt)
		if rerr == nil && liftsAt != nil && liftsAt.Before(time.Now()) {
			_, _ = a.db.Pool().Exec(ctx, `UPDATE member_restrictions SET lifted_at=NOW(),updated_at=NOW() WHERE account_id=$1 AND lifted_at IS NULL AND lifts_at<=NOW()`, accountID)
			_, _ = a.db.Pool().Exec(ctx, `UPDATE accounts SET status='active',updated_at=NOW() WHERE id=$1`, accountID)
			status = "active"
		} else {
			payload := map[string]any{"ok": false, "error": "This member account is restricted.", "reason": reason, "restricted": true, "permanent": liftsAt == nil}
			if liftsAt != nil {
				payload["lifts_at"] = liftsAt.In(philippineTime).Format(time.RFC3339)
			}
			writeJSON(w, http.StatusForbidden, payload)
			return
		}
	}
	if status != "active" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "this member account is unavailable"})
		return
	}
	member, err := a.loadMemberIdentity(ctx, accountID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "member profile unavailable"})
		return
	}

	token, err := randomToken(36)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "could not create session"})
		return
	}
	member.ExpiresAt = time.Now().Add(7 * 24 * time.Hour)
	tokenHash := oauthSecretHash(token)
	if _, err = a.db.Pool().Exec(ctx, `
		INSERT INTO member_sessions(token_hash,account_id,expires_at,last_seen_at)
		VALUES($1,$2,$3,NOW())
		ON CONFLICT(token_hash) DO UPDATE SET account_id=EXCLUDED.account_id,expires_at=EXCLUDED.expires_at,last_seen_at=NOW()`, tokenHash, accountID, member.ExpiresAt); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "could not create session"})
		return
	}
	_, _ = a.db.Pool().Exec(ctx, `DELETE FROM member_sessions WHERE expires_at<=NOW()`)
	a.mu.Lock()
	a.memberSessions[token] = member
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "aits_member_session", Value: token, Path: "/", HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600, Expires: member.ExpiresAt})
	_, _ = a.db.Pool().Exec(ctx, `UPDATE accounts SET last_login_at=NOW(), updated_at=NOW() WHERE id=$1`, accountID)
	writeJSON(w, 200, map[string]any{"ok": true, "member": memberPayload(member)})
}

func isDevelopmentTestUsername(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "aitsdeveloper", "aitsinstructor", "aitspresident", "aitsvicepresident", "aitssecretary", "aitstreasurer", "aitspio", "aitsmayor", "aitsmember":
		return true
	default:
		return false
	}
}

func isLocalDevelopmentRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	for _, raw := range []string{r.Header.Get("Origin"), r.Header.Get("Referer")} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err == nil {
			host := strings.ToLower(u.Hostname())
			if host == "127.0.0.1" || host == "localhost" {
				return true
			}
		}
	}
	for _, raw := range []string{r.Header.Get("X-Forwarded-Host"), r.Host} {
		host := strings.ToLower(strings.TrimSpace(strings.Split(raw, ",")[0]))
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host == "127.0.0.1" || host == "localhost" {
			return true
		}
	}
	return false
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func (a *appServer) memberLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("aits_member_session"); err == nil {
		a.mu.Lock()
		delete(a.memberSessions, c.Value)
		a.mu.Unlock()
		if a.db != nil {
			_, _ = a.db.Pool().Exec(r.Context(), `DELETE FROM member_sessions WHERE token_hash=$1`, oauthSecretHash(c.Value))
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "aits_member_session", Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteLaxMode})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *appServer) currentMember(r *http.Request) (memberSessionEntry, bool) {
	c, err := r.Cookie("aits_member_session")
	if err != nil || c.Value == "" {
		return memberSessionEntry{}, false
	}
	a.mu.RLock()
	entry, ok := a.memberSessions[c.Value]
	a.mu.RUnlock()
	if ok && time.Now().Before(entry.ExpiresAt) {
		return entry, true
	}
	if ok {
		a.mu.Lock()
		delete(a.memberSessions, c.Value)
		a.mu.Unlock()
	}
	if a.db == nil {
		return memberSessionEntry{}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	var accountID int64
	var expiresAt time.Time
	err = a.db.Pool().QueryRow(ctx, `SELECT account_id,expires_at FROM member_sessions WHERE token_hash=$1 AND expires_at>NOW()`, oauthSecretHash(c.Value)).Scan(&accountID, &expiresAt)
	if err != nil {
		return memberSessionEntry{}, false
	}
	entry, err = a.loadMemberIdentity(ctx, accountID)
	if err != nil {
		return memberSessionEntry{}, false
	}
	entry.ExpiresAt = expiresAt
	a.mu.Lock()
	a.memberSessions[c.Value] = entry
	a.mu.Unlock()
	_, _ = a.db.Pool().Exec(ctx, `UPDATE member_sessions SET last_seen_at=NOW() WHERE token_hash=$1`, oauthSecretHash(c.Value))
	return entry, true
}

func memberPayload(m memberSessionEntry) map[string]any {
	return map[string]any{
		"account_id": m.AccountID, "name": m.Name, "username": m.Username, "email": m.Email, "avatar": m.Avatar,
		"role": m.Role, "role_name": m.RoleName, "division": m.Division, "division_id": m.DivisionID,
		"membership_level": m.MembershipLevel, "is_admin": m.IsAdmin,
		"tags": []map[string]string{{"label": "Division", "value": firstNonEmpty(m.Division, "AITS")}, {"label": "Access", "value": firstNonEmpty(m.RoleName, "Member")}, {"label": "Membership", "value": firstNonEmpty(m.MembershipLevel, "Member")}},
	}
}

func (a *appServer) loadMemberIdentity(ctx context.Context, accountID int64) (memberSessionEntry, error) {
	var m memberSessionEntry
	var divisionID *int64
	err := a.db.Pool().QueryRow(ctx, `
        SELECT a.id, a.display_name, COALESCE(a.username,''), COALESCE(a.email,''), COALESCE(a.avatar_url,''),
               mp.primary_division_id, COALESCE(d.name,''), COALESCE(mp.membership_level,'Member'),
               COALESCE((SELECT r.code FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE ar.account_id=a.id AND ar.is_active=TRUE AND (ar.expires_at IS NULL OR ar.expires_at>NOW()) ORDER BY r.hierarchy_level DESC LIMIT 1),'member'),
               COALESCE((SELECT r.name FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE ar.account_id=a.id AND ar.is_active=TRUE AND (ar.expires_at IS NULL OR ar.expires_at>NOW()) ORDER BY r.hierarchy_level DESC LIMIT 1),'Member'),
               EXISTS(SELECT 1 FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE ar.account_id=a.id AND ar.is_active=TRUE AND r.is_administrator=TRUE AND (ar.expires_at IS NULL OR ar.expires_at>NOW()))
        FROM accounts a
        LEFT JOIN member_profiles mp ON mp.account_id=a.id
        LEFT JOIN divisions d ON d.id=mp.primary_division_id
        WHERE a.id=$1 AND a.account_kind='member' AND a.status='active'`, accountID).Scan(&m.AccountID, &m.Name, &m.Username, &m.Email, &m.Avatar, &divisionID, &m.Division, &m.MembershipLevel, &m.Role, &m.RoleName, &m.IsAdmin)
	if err != nil {
		return m, err
	}
	if divisionID != nil {
		m.DivisionID = *divisionID
	}
	return m, nil
}

func (a *appServer) memberSession(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 200, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, 200, map[string]any{"authenticated": true, "member": memberPayload(m)})
}

func (a *appServer) memberMe(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var department, course, year, biography, gender, contact, facebook, instagram, github, first, middle, last, ext, schoolID, memberNumber string
	var birthDate *time.Time
	err := a.db.Pool().QueryRow(ctx, `SELECT COALESCE(department,''),COALESCE(course,''),COALESCE(year_level,''),COALESCE(biography,''),birth_date,COALESCE(gender,''),COALESCE(contact_number,''),COALESCE(facebook_url,''),COALESCE(instagram_url,''),COALESCE(github_url,''),COALESCE(first_name,''),COALESCE(middle_name,''),COALESCE(last_name,''),COALESCE(extension_name,''),COALESCE(school_id,''),COALESCE(member_number,'') FROM member_profiles WHERE account_id=$1`, m.AccountID).Scan(&department, &course, &year, &biography, &birthDate, &gender, &contact, &facebook, &instagram, &github, &first, &middle, &last, &ext, &schoolID, &memberNumber)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "profile unavailable"})
		return
	}
	payload := memberPayload(m)
	payload["department"] = department
	payload["course"] = course
	payload["year_level"] = year
	payload["biography"] = biography
	payload["gender"] = gender
	payload["contact_number"] = contact
	payload["facebook_url"] = facebook
	payload["instagram_url"] = instagram
	payload["github_url"] = github
	payload["first_name"] = first
	payload["middle_name"] = middle
	payload["last_name"] = last
	payload["extension_name"] = ext
	payload["school_id"] = firstNonEmpty(schoolID, memberNumber)
	if birthDate != nil {
		payload["birth_date"] = birthDate.Format("2006-01-02")
	} else {
		payload["birth_date"] = ""
	}
	tags := []string{}
	if rows, qerr := a.db.Pool().Query(ctx, `SELECT tag FROM member_tags WHERE account_id=$1 ORDER BY sort_order,tag`, m.AccountID); qerr == nil {
		defer rows.Close()
		for rows.Next() {
			var tag string
			if rows.Scan(&tag) == nil {
				tags = append(tags, tag)
			}
		}
	}
	payload["member_tags"] = tags
	var pendingDivisionID *int64
	var pendingDivision string
	_ = a.db.Pool().QueryRow(ctx, `SELECT r.requested_division_id,COALESCE(d.name,'') FROM member_change_requests r LEFT JOIN divisions d ON d.id=r.requested_division_id WHERE r.account_id=$1 AND r.request_type='division' AND r.status='pending' ORDER BY r.id DESC LIMIT 1`, m.AccountID).Scan(&pendingDivisionID, &pendingDivision)
	if pendingDivisionID != nil {
		payload["pending_division_request"] = map[string]any{"division_id": *pendingDivisionID, "division": pendingDivision}
	} else {
		payload["pending_division_request"] = nil
	}
	var pendingMembership string
	_ = a.db.Pool().QueryRow(ctx, `SELECT COALESCE(requested_membership,'') FROM member_change_requests WHERE account_id=$1 AND request_type='membership' AND status='pending' ORDER BY id DESC LIMIT 1`, m.AccountID).Scan(&pendingMembership)
	payload["pending_membership_request"] = pendingMembership
	writeJSON(w, 200, map[string]any{"member": payload})
}

var memberUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)

func (a *appServer) updateMemberMe(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	var req struct {
		FirstName     string `json:"first_name"`
		MiddleName    string `json:"middle_name"`
		LastName      string `json:"last_name"`
		ExtensionName string `json:"extension_name"`
		Username      string `json:"username"`
		YearLevel     string `json:"year_level"`
		BirthDate     string `json:"birth_date"`
		Gender        string `json:"gender"`
		Course        string `json:"course"`
		Department    string `json:"department"`
		SchoolID      string `json:"school_id"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 48<<10)).Decode(&req) != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid request"})
		return
	}
	for _, p := range []*string{&req.FirstName, &req.MiddleName, &req.LastName, &req.ExtensionName, &req.Username, &req.YearLevel, &req.BirthDate, &req.Gender, &req.Course, &req.Department, &req.SchoolID} {
		*p = strings.TrimSpace(*p)
	}
	if req.FirstName == "" || req.LastName == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "first and last name are required"})
		return
	}
	if !memberUsernamePattern.MatchString(req.Username) {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "username must be 3-32 characters using letters, numbers, dot, dash, or underscore"})
		return
	}
	if req.BirthDate != "" {
		birth, err := time.Parse("2006-01-02", req.BirthDate)
		if err != nil || birth.After(time.Now()) {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "enter a valid birthdate"})
			return
		}
	}
	full := req.FirstName
	if req.MiddleName != "" {
		runes := []rune(req.MiddleName)
		if len(runes) > 0 {
			full += " " + strings.ToUpper(string(runes[0])) + "."
		}
	}
	full += " " + req.LastName
	if req.ExtensionName != "" {
		full += " " + req.ExtensionName
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "profile update unavailable"})
		return
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE accounts SET display_name=$1,username=$2,updated_at=NOW() WHERE id=$3`, full, req.Username, m.AccountID); err != nil {
		writeJSON(w, 409, map[string]any{"ok": false, "error": "that username is already in use"})
		return
	}
	_, err = tx.Exec(ctx, `UPDATE member_profiles SET first_name=$1,middle_name=NULLIF($2,''),last_name=$3,extension_name=NULLIF($4,''),department=NULLIF($5,''),course=NULLIF($6,''),year_level=NULLIF($7,''),birth_date=$8::date,gender=NULLIF($9,''),school_id=NULLIF($10,''),updated_at=NOW() WHERE account_id=$11`, req.FirstName, req.MiddleName, req.LastName, req.ExtensionName, req.Department, req.Course, req.YearLevel, nullableDate(req.BirthDate), req.Gender, req.SchoolID, m.AccountID)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "one or more profile details are invalid"})
		return
	}
	// Member-authored posts follow the account identity instead of keeping a stale name snapshot.
	// Branded organization/division posts intentionally retain their own author label.
	if _, err = tx.Exec(ctx, `UPDATE posts SET author_label=$1,updated_at=NOW() WHERE author_account_id=$2 AND COALESCE(identity_kind,'member')='member'`, full, m.AccountID); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "profile identity could not be synchronized"})
		return
	}
	if tx.Commit(ctx) != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "profile could not be saved"})
		return
	}
	m.Name = full
	m.Username = req.Username
	a.refreshMemberSession(r, m)
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Profile saved."})
}

func (a *appServer) memberDivisions(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentMember(r); !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	rows, err := a.db.Pool().Query(r.Context(), `SELECT id,code,name,COALESCE(description,'') FROM divisions WHERE is_active=TRUE ORDER BY sort_order,id`)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var code, name, description string
		if rows.Scan(&id, &code, &name, &description) == nil {
			items = append(items, map[string]any{"id": id, "code": code, "name": name, "description": description})
		}
	}
	writeJSON(w, 200, map[string]any{"divisions": items})
}

func (a *appServer) memberDivisionRequest(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	var req struct {
		DivisionID int64 `json:"division_id"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req) != nil || req.DivisionID < 1 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "choose a division"})
		return
	}
	var exists bool
	_ = a.db.Pool().QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM divisions WHERE id=$1 AND is_active=TRUE)`, req.DivisionID).Scan(&exists)
	if !exists {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "selected division is unavailable"})
		return
	}
	if m.DivisionID == req.DivisionID {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "you are already in this division"})
		return
	}
	_, err := a.db.Pool().Exec(r.Context(), `INSERT INTO member_change_requests(account_id,request_type,requested_division_id,status) VALUES($1,'division',$2,'pending') ON CONFLICT (account_id,request_type) WHERE status='pending' DO UPDATE SET requested_division_id=EXCLUDED.requested_division_id,updated_at=NOW()`, m.AccountID, req.DivisionID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "could not submit division request"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Division request submitted for review."})
}

func validEmailAddress(value string) bool {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	return err == nil && strings.EqualFold(address.Address, strings.TrimSpace(value))
}

func generateEmailCode() string {
	token, err := randomToken(10)
	if err != nil {
		return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	}
	var total int
	for _, r := range token {
		total = (total*33 + int(r)) % 1000000
	}
	return fmt.Sprintf("%06d", total)
}

type securityCodeRequestError struct {
	Message    string
	RetryAfter int64
}

func (e *securityCodeRequestError) Error() string { return e.Message }

// createAndSendSecurityCode enforces an independent request window per purpose.
// A live code cannot be replaced: it must be used or allowed to expire first.
// Each purpose is limited to five issued codes per rolling two-hour window.
func (a *appServer) createAndSendSecurityCode(ctx context.Context, accountID int64, email, purpose, greeting string, payload map[string]string, enforceGate bool) error {
	if !a.config.SMTPReady() {
		return fmt.Errorf("email service is not available")
	}
	email = strings.TrimSpace(email)
	if !validEmailAddress(email) {
		return fmt.Errorf("account email is unavailable")
	}
	code := generateEmailCode()
	hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	payloadJSON, _ := json.Marshal(payload)
	if len(payloadJSON) == 0 || string(payloadJSON) == "null" {
		payloadJSON = []byte(`{}`)
	}

	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Lock the member row so two tabs cannot create overlapping codes for the
	// same account at the same instant.
	var lockedID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, accountID).Scan(&lockedID); err != nil {
		return err
	}
	if enforceGate {
		var activeRetry int64
		err = tx.QueryRow(ctx, `
			SELECT GREATEST(1, CEIL(EXTRACT(EPOCH FROM (expires_at-NOW()))))::bigint
			FROM account_security_codes
			WHERE account_id=$1 AND purpose=$2 AND used_at IS NULL AND expires_at>NOW()
			ORDER BY created_at DESC LIMIT 1`, accountID, purpose).Scan(&activeRetry)
		if err == nil {
			return &securityCodeRequestError{Message: "A verification code is already active. Use it or wait for it to expire.", RetryAfter: activeRetry}
		}
		if err != pgx.ErrNoRows {
			return err
		}
		var recentCount int
		if err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM account_security_codes WHERE account_id=$1 AND purpose=$2 AND created_at>NOW()-INTERVAL '2 hours'`, accountID, purpose).Scan(&recentCount); err != nil {
			return err
		}
		if recentCount >= 5 {
			var retry int64
			if err = tx.QueryRow(ctx, `
				SELECT GREATEST(1, CEIL(EXTRACT(EPOCH FROM (MIN(created_at)+INTERVAL '2 hours'-NOW()))))::bigint
				FROM account_security_codes
				WHERE account_id=$1 AND purpose=$2 AND created_at>NOW()-INTERVAL '2 hours'`, accountID, purpose).Scan(&retry); err != nil {
				return err
			}
			return &securityCodeRequestError{Message: "You have reached the 5-request limit for this security action. Try again later.", RetryAfter: retry}
		}
	}

	var codeID int64
	if err = tx.QueryRow(ctx, `INSERT INTO account_security_codes(account_id,purpose,code_hash,expires_at,payload) VALUES($1,$2,$3,NOW()+INTERVAL '15 minutes',$4::jsonb) RETURNING id`, accountID, purpose, string(hash), string(payloadJSON)).Scan(&codeID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if err = a.mailer.SendVerification(email, greeting, code, purpose); err != nil {
		// A failed delivery must not consume the active-code slot or request quota.
		_, _ = a.db.Pool().Exec(ctx, `DELETE FROM account_security_codes WHERE id=$1 AND used_at IS NULL`, codeID)
		return err
	}
	return nil
}

func (a *appServer) createAndSendPasswordCode(ctx context.Context, accountID int64, email, purpose, greeting string) error {
	return a.createAndSendSecurityCode(ctx, accountID, email, purpose, greeting, nil, purpose == "password_change")
}

func writeSecurityCodeRequestError(w http.ResponseWriter, err error, fallback string) {
	if limited, ok := err.(*securityCodeRequestError); ok {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": limited.Message, "retry_after_seconds": limited.RetryAfter})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": fallback})
}

func (a *appServer) requestMemberPasswordChange(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	if strings.TrimSpace(m.Email) == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "add an email to your account before changing your password"})
		return
	}
	if err := a.createAndSendPasswordCode(r.Context(), m.AccountID, m.Email, "password_change", m.Name); err != nil {
		writeSecurityCodeRequestError(w, err, "verification email could not be sent")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "A 15-minute verification code was sent to your account email.", "expires_in_seconds": 900})
}

func (a *appServer) confirmMemberPasswordChange(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	var req struct{ Code, NewPassword string }
	if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req) != nil || len(req.NewPassword) < 10 || strings.TrimSpace(req.Code) == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "enter the verification code and a password with at least 10 characters"})
		return
	}
	var id int64
	var codeHash string
	err := a.db.Pool().QueryRow(r.Context(), `SELECT id,code_hash FROM account_security_codes WHERE account_id=$1 AND purpose='password_change' AND used_at IS NULL AND expires_at>NOW() ORDER BY id DESC LIMIT 1`, m.AccountID).Scan(&id, &codeHash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(codeHash), []byte(strings.TrimSpace(req.Code))) != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "verification code is invalid or expired"})
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	tx, err := a.db.Pool().Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `UPDATE accounts SET password_hash=$1,updated_at=NOW() WHERE id=$2`, string(newHash), m.AccountID); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	_, _ = tx.Exec(r.Context(), `UPDATE account_security_codes SET used_at=NOW(),updated_at=NOW() WHERE id=$1`, id)
	if tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	keepToken := ""
	if c, cookieErr := r.Cookie("aits_member_session"); cookieErr == nil {
		keepToken = c.Value
	}
	a.invalidateMemberSessions(r.Context(), m.AccountID, keepToken)
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Password updated."})
}

func (a *appServer) requestPasswordRecovery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Identifier string `json:"identifier"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req)
	identifier := strings.TrimSpace(req.Identifier)
	if identifier == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "enter your username or email"})
		return
	}
	var id int64
	var email, name string
	err := a.db.Pool().QueryRow(r.Context(), `SELECT id,COALESCE(email,''),display_name FROM accounts WHERE account_kind='member' AND status='active' AND (LOWER(username)=LOWER($1) OR LOWER(COALESCE(email,''))=LOWER($1)) LIMIT 1`, identifier).Scan(&id, &email, &name)
	if err == nil && email != "" {
		_ = a.createAndSendPasswordCode(r.Context(), id, email, "password_recovery", name)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "If the account has an email, a verification code has been sent."})
}

func (a *appServer) confirmPasswordRecovery(w http.ResponseWriter, r *http.Request) {
	var req struct{ Identifier, Code, NewPassword string }
	if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req) != nil || strings.TrimSpace(req.Identifier) == "" || strings.TrimSpace(req.Code) == "" || len(req.NewPassword) < 10 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "enter your account, verification code, and a password with at least 10 characters"})
		return
	}
	var accountID, codeID int64
	var codeHash string
	err := a.db.Pool().QueryRow(r.Context(), `SELECT a.id,c.id,c.code_hash FROM accounts a JOIN LATERAL (SELECT id,code_hash FROM account_security_codes WHERE account_id=a.id AND purpose='password_recovery' AND used_at IS NULL AND expires_at>NOW() ORDER BY id DESC LIMIT 1) c ON TRUE WHERE a.account_kind='member' AND (LOWER(a.username)=LOWER($1) OR LOWER(COALESCE(a.email,''))=LOWER($1)) LIMIT 1`, strings.TrimSpace(req.Identifier)).Scan(&accountID, &codeID, &codeHash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(codeHash), []byte(strings.TrimSpace(req.Code))) != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "verification code is invalid or expired"})
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	tx, err := a.db.Pool().Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `UPDATE accounts SET password_hash=$1,updated_at=NOW() WHERE id=$2`, string(newHash), accountID); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	_, _ = tx.Exec(r.Context(), `UPDATE account_security_codes SET used_at=NOW(),updated_at=NOW() WHERE id=$1`, codeID)
	if tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	a.invalidateMemberSessions(r.Context(), accountID, "")
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Password reset complete. You can sign in now."})
}

func (a *appServer) invalidateMemberSessions(ctx context.Context, accountID int64, keepToken string) {
	if accountID <= 0 {
		return
	}
	if a.db != nil {
		if keepToken != "" {
			_, _ = a.db.Pool().Exec(ctx, `DELETE FROM member_sessions WHERE account_id=$1 AND token_hash<>$2`, accountID, oauthSecretHash(keepToken))
		} else {
			_, _ = a.db.Pool().Exec(ctx, `DELETE FROM member_sessions WHERE account_id=$1`, accountID)
		}
	}
	a.mu.Lock()
	for token, session := range a.memberSessions {
		if session.AccountID == accountID && token != keepToken {
			delete(a.memberSessions, token)
		}
	}
	a.mu.Unlock()
}

func (a *appServer) refreshMemberSession(r *http.Request, m memberSessionEntry) {
	c, err := r.Cookie("aits_member_session")
	if err != nil {
		return
	}
	m.ExpiresAt = time.Now().Add(7 * 24 * time.Hour)
	a.mu.Lock()
	a.memberSessions[c.Value] = m
	a.mu.Unlock()
	if a.db != nil {
		_, _ = a.db.Pool().Exec(r.Context(), `UPDATE member_sessions SET expires_at=$2,last_seen_at=NOW() WHERE token_hash=$1`, oauthSecretHash(c.Value), m.ExpiresAt)
	}
}

func (a *appServer) memberNotifications(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := a.db.Pool().Query(ctx, `SELECT id,notification_type,title,COALESCE(body,''),COALESCE(action_url,''),read_at,created_at FROM notifications WHERE account_id=$1 ORDER BY created_at DESC LIMIT 100`, m.AccountID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var typ, title, body, actionURL string
		var readAt *time.Time
		var created time.Time
		if rows.Scan(&id, &typ, &title, &body, &actionURL, &readAt, &created) != nil {
			continue
		}
		items = append(items, map[string]any{"id": id, "type": typ, "title": title, "body": body, "action_url": actionURL, "time": formatPublicDateTime(created), "read": readAt != nil})
	}
	writeJSON(w, 200, map[string]any{"notifications": items})
}

func (a *appServer) markNotificationsRead(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false})
		return
	}
	_, err := a.db.Pool().Exec(r.Context(), `UPDATE notifications SET read_at=COALESCE(read_at,NOW()), updated_at=NOW() WHERE account_id=$1`, m.AccountID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *appServer) memberDirectory(w http.ResponseWriter, r *http.Request) {
	_, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := a.db.Pool().Query(ctx, `
		SELECT a.id,a.display_name,COALESCE(a.username,''),COALESCE(a.avatar_url,''),
		       COALESCE(d.name,'AITS'),COALESCE(mp.membership_level,'Member'),
		       COALESCE((SELECT r.name FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE ar.account_id=a.id AND ar.is_active=TRUE ORDER BY r.hierarchy_level DESC LIMIT 1),'Member'),
		       COALESCE((SELECT string_agg(mt.tag,E'\x1f' ORDER BY mt.sort_order,mt.tag) FROM member_tags mt WHERE mt.account_id=a.id),''),
		       COALESCE((SELECT SUM(ps.seconds_online) FROM member_presence_sessions ps WHERE ps.account_id=a.id),0),
		       (SELECT MAX(COALESCE(ps.disconnected_at,ps.last_seen_at)) FROM member_presence_sessions ps WHERE ps.account_id=a.id)
		FROM accounts a
		JOIN member_profiles mp ON mp.account_id=a.id
		LEFT JOIN divisions d ON d.id=mp.primary_division_id
		WHERE a.account_kind='member' AND a.status='active' AND LOWER(COALESCE(a.username,'')) <> 'aitsdeveloper'
		ORDER BY a.display_name`)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer rows.Close()
	online := a.presence.snapshot()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var name, username, avatar, division, membership, access, tagsText string
		var totalSeconds int64
		var lastSeen *time.Time
		if rows.Scan(&id, &name, &username, &avatar, &division, &membership, &access, &tagsText, &totalSeconds, &lastSeen) != nil {
			continue
		}
		tags := make([]string, 0)
		if tagsText != "" {
			tags = strings.Split(tagsText, "\x1f")
		}
		presence, isOnline := online[strings.ToLower(username)]
		straightSeconds := int64(0)
		connectedAt := ""
		if isOnline {
			connectedAt = presence.ConnectedAt
			if connected, err := time.Parse(time.RFC3339, presence.ConnectedAt); err == nil {
				straightSeconds = int64(time.Since(connected).Seconds())
				totalSeconds += straightSeconds
			}
		}
		lastSeenAt := ""
		if lastSeen != nil {
			lastSeenAt = lastSeen.In(philippineTime).Format(time.RFC3339)
		}
		items = append(items, map[string]any{"account_id": id, "name": name, "username": username, "avatar": avatar, "division": division, "access": access, "membership": membership, "tags": tags, "online": isOnline, "online_seconds": totalSeconds, "straight_online_seconds": straightSeconds, "connected_at": connectedAt, "last_seen_at": lastSeenAt})
	}
	writeJSON(w, 200, map[string]any{"members": items})
}

func (a *appServer) memberPresence(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		http.Error(w, "member login required", http.StatusUnauthorized)
		return
	}
	var sessionID int64
	start := time.Now()
	_ = a.db.Pool().QueryRow(r.Context(), `INSERT INTO member_presence_sessions(account_id,connected_at,last_seen_at) VALUES($1,NOW(),NOW()) RETURNING id`, m.AccountID).Scan(&sessionID)
	a.presence.handleMember(w, r, presenceMember{Name: m.Name, Username: m.Username, Division: firstNonEmpty(m.Division, "AITS"), Role: firstNonEmpty(m.RoleName, "Member")})
	seconds := int64(time.Since(start).Seconds())
	if sessionID > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = a.db.Pool().Exec(ctx, `UPDATE member_presence_sessions SET disconnected_at=NOW(),last_seen_at=NOW(),seconds_online=$1 WHERE id=$2`, seconds, sessionID)
	}
}

func (a *appServer) refreshMemberSessionsForAccount(ctx context.Context, accountID int64) {
	fresh, err := a.loadMemberIdentity(ctx, accountID)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for token, session := range a.memberSessions {
		if session.AccountID != accountID {
			continue
		}
		fresh.ExpiresAt = session.ExpiresAt
		a.memberSessions[token] = fresh
	}
}
