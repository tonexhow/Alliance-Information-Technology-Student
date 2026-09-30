package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func (a *appServer) requireAdmin(w http.ResponseWriter, r *http.Request) (memberSessionEntry, bool) {
	m, ok := a.currentMember(r)
	if !ok || !m.IsAdmin {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "administrator login required"})
		return m, false
	}
	return m, true
}

func (a *appServer) adminOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	counts := map[string]int64{}
	queries := map[string]string{"members": `SELECT COUNT(*) FROM accounts WHERE account_kind='member' AND status='active'`, "applications": `SELECT COUNT(*) FROM applications`, "posts": `SELECT COUNT(*) FROM posts WHERE moderation_status='published' AND deleted_at IS NULL`, "pending_posts": `SELECT COUNT(*) FROM posts WHERE moderation_status='pending' AND deleted_at IS NULL`, "announcements": `SELECT COUNT(*) FROM announcements WHERE is_published=TRUE`, "visitors_today": `SELECT COUNT(*) FROM analytics_events WHERE event_type='page_view' AND created_at>=CURRENT_DATE`, "interactions_today": `SELECT COUNT(*) FROM analytics_events WHERE event_type IN ('post_react','post_comment','post_share') AND created_at>=CURRENT_DATE`}
	for key, q := range queries {
		var n int64
		_ = a.db.Pool().QueryRow(ctx, q).Scan(&n)
		counts[key] = n
	}
	counts["online_members"] = int64(len(a.presence.snapshot()))
	applications := []map[string]any{}
	if rows, err := a.db.Pool().Query(ctx, `SELECT to_char(day,'Mon DD'),COALESCE(x.n,0) FROM generate_series(CURRENT_DATE-INTERVAL '13 days',CURRENT_DATE,INTERVAL '1 day') day LEFT JOIN (SELECT DATE(created_at) d,COUNT(*) n FROM applications WHERE created_at>=CURRENT_DATE-INTERVAL '13 days' GROUP BY DATE(created_at)) x ON x.d=day::date ORDER BY day`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var label string
			var n int64
			if rows.Scan(&label, &n) == nil {
				applications = append(applications, map[string]any{"label": label, "value": n})
			}
		}
	}
	divisions := []map[string]any{}
	if rows, err := a.db.Pool().Query(ctx, `SELECT COALESCE(d.name,'Unassigned'),COUNT(*) FROM member_profiles mp LEFT JOIN divisions d ON d.id=mp.primary_division_id JOIN accounts a ON a.id=mp.account_id WHERE a.status='active' GROUP BY d.name ORDER BY COUNT(*) DESC`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var label string
			var n int64
			if rows.Scan(&label, &n) == nil {
				divisions = append(divisions, map[string]any{"label": label, "value": n})
			}
		}
	}
	onlineTop := []map[string]any{}
	if rows, err := a.db.Pool().Query(ctx, `SELECT a.display_name,COALESCE(SUM(ps.seconds_online),0) total FROM member_presence_sessions ps JOIN accounts a ON a.id=ps.account_id GROUP BY a.id,a.display_name ORDER BY total DESC LIMIT 8`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var label string
			var n int64
			if rows.Scan(&label, &n) == nil {
				onlineTop = append(onlineTop, map[string]any{"label": label, "value": n})
			}
		}
	}
	activity := []map[string]any{}
	if rows, err := a.db.Pool().Query(ctx, `SELECT a.display_name,(COALESCE(p.n,0)+COALESCE(c.n,0)+COALESCE(r.n,0)) score FROM accounts a LEFT JOIN (SELECT author_account_id id,COUNT(*) n FROM posts GROUP BY author_account_id)p ON p.id=a.id LEFT JOIN (SELECT account_id id,COUNT(*) n FROM post_comments GROUP BY account_id)c ON c.id=a.id LEFT JOIN (SELECT account_id id,COUNT(*) n FROM post_reactions GROUP BY account_id)r ON r.id=a.id WHERE a.account_kind='member' ORDER BY score DESC,a.display_name LIMIT 8`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var label string
			var n int64
			if rows.Scan(&label, &n) == nil {
				activity = append(activity, map[string]any{"label": label, "value": n})
			}
		}
	}
	activityByDay := []map[string]any{}
	if rows, err := a.db.Pool().Query(ctx, `SELECT to_char(day,'Mon DD'),COALESCE(x.visits,0),COALESCE(x.interactions,0),COALESCE(x.posts,0) FROM generate_series(CURRENT_DATE-INTERVAL '13 days',CURRENT_DATE,INTERVAL '1 day') day LEFT JOIN (SELECT DATE(created_at) d,COUNT(*) FILTER (WHERE event_type='page_view') visits,COUNT(*) FILTER (WHERE event_type IN ('post_react','post_comment','post_share')) interactions,COUNT(*) FILTER (WHERE event_type='post_create') posts FROM analytics_events WHERE created_at>=CURRENT_DATE-INTERVAL '13 days' GROUP BY DATE(created_at)) x ON x.d=day::date ORDER BY day`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var day string
			var visits, interactions, posts int64
			if rows.Scan(&day, &visits, &interactions, &posts) == nil {
				activityByDay = append(activityByDay, map[string]any{"label": day, "visits": visits, "interactions": interactions, "posts": posts})
			}
		}
	}
	writeJSON(w, 200, map[string]any{"counts": counts, "applications_by_day": applications, "activity_by_day": activityByDay, "division_mix": divisions, "top_online": onlineTop, "top_activity": activity})
}

func (a *appServer) adminDivisions(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := a.db.Pool().Query(ctx, `SELECT d.id,d.code,d.name,COALESCE(d.description,''),COALESCE(d.logo_url,''),d.sort_order,d.is_active,(SELECT COUNT(*) FROM member_profiles mp JOIN accounts a ON a.id=mp.account_id WHERE mp.primary_division_id=d.id AND a.status='active') FROM divisions d ORDER BY d.sort_order,d.id`)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, count int64
		var sortOrder int
		var code, name, desc, logoURL string
		var active bool
		if rows.Scan(&id, &code, &name, &desc, &logoURL, &sortOrder, &active, &count) == nil {
			items = append(items, map[string]any{"id": id, "code": code, "name": name, "description": desc, "logo_url": logoURL, "sort_order": sortOrder, "active": active, "member_count": count})
		}
	}
	writeJSON(w, 200, map[string]any{"divisions": items})
}

func (a *appServer) adminMembers(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		a.adminCreateMember(w, r, actor)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	rows, err := a.db.Pool().Query(ctx, `
        SELECT a.id,a.display_name,COALESCE(a.username,''),COALESCE(a.email,''),COALESCE(a.avatar_url,''),
               COALESCE(d.id,0),COALESCE(d.name,'AITS'),COALESCE(mp.membership_level,'Member'),COALESCE(mp.gender,''),mp.birth_date,
               COALESCE(mp.contact_number,''),COALESCE(mp.facebook_url,''),COALESCE(mp.instagram_url,''),COALESCE(mp.github_url,''),
               COALESCE(mp.department,''),COALESCE(mp.course,''),COALESCE(mp.year_level,''),
               COALESCE((SELECT r.id FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE ar.account_id=a.id AND ar.is_active=TRUE ORDER BY r.hierarchy_level DESC LIMIT 1),0),
               COALESCE((SELECT r.code FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE ar.account_id=a.id AND ar.is_active=TRUE ORDER BY r.hierarchy_level DESC LIMIT 1),'member'),
               COALESCE((SELECT r.name FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE ar.account_id=a.id AND ar.is_active=TRUE ORDER BY r.hierarchy_level DESC LIMIT 1),'Member'),
               COALESCE((SELECT string_agg(mt.tag,E'\x1f' ORDER BY mt.sort_order,mt.tag) FROM member_tags mt WHERE mt.account_id=a.id),'')
        FROM accounts a
        JOIN member_profiles mp ON mp.account_id=a.id
        LEFT JOIN divisions d ON d.id=mp.primary_division_id
        WHERE a.account_kind='member' AND LOWER(COALESCE(a.username,'')) <> 'aitsdeveloper'
        ORDER BY a.display_name`)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, divisionID, roleID int64
		var name, user, email, avatar, division, membership, gender, contact, fb, ig, gh, department, course, year, roleCode, roleName, tagsText string
		var birth *time.Time
		if rows.Scan(&id, &name, &user, &email, &avatar, &divisionID, &division, &membership, &gender, &birth, &contact, &fb, &ig, &gh, &department, &course, &year, &roleID, &roleCode, &roleName, &tagsText) == nil {
			birthText := ""
			if birth != nil {
				birthText = birth.Format("2006-01-02")
			}
			tags := make([]string, 0)
			if tagsText != "" {
				tags = strings.Split(tagsText, "\x1f")
			}
			items = append(items, map[string]any{
				"account_id": id, "name": name, "username": user, "email": email, "avatar": avatar,
				"division_id": divisionID, "division": division, "membership": membership, "gender": gender, "birth_date": birthText,
				"contact_number": contact, "facebook_url": fb, "instagram_url": ig, "github_url": gh,
				"department": department, "course": course, "year_level": year,
				"role_id": roleID, "role_code": roleCode, "role": roleName, "tags": tags,
			})
		}
	}
	writeJSON(w, 200, map[string]any{"members": items})
}

func (a *appServer) adminPosts(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	if r.Method == http.MethodGet {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		rows, err := a.db.Pool().Query(ctx, `SELECT p.id,COALESCE(p.title,''),p.body,p.visibility,p.moderation_status,CASE WHEN COALESCE(p.identity_kind,'member')='member' THEN a.display_name ELSE COALESCE(NULLIF(p.author_label,''),a.display_name) END,COALESCE(d.id,0),COALESCE(d.name,''),p.media::text,COALESCE(p.published_at,p.created_at) FROM posts p JOIN accounts a ON a.id=p.author_account_id LEFT JOIN divisions d ON d.id=p.division_id WHERE p.deleted_at IS NULL ORDER BY p.created_at DESC LIMIT 200`)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		defer rows.Close()
		items := make([]map[string]any, 0)
		for rows.Next() {
			var id, divisionID int64
			var title, body, visibility, status, author, division, rawMedia string
			var t time.Time
			if rows.Scan(&id, &title, &body, &visibility, &status, &author, &divisionID, &division, &rawMedia, &t) == nil {
				media := []chatAttachment{}
				_ = json.Unmarshal([]byte(rawMedia), &media)
				items = append(items, map[string]any{"id": id, "title": title, "body": body, "visibility": visibility, "status": status, "author": author, "division_id": divisionID, "division": division, "media": media, "time": formatPublicDateTime(t)})
			}
		}
		writeJSON(w, 200, map[string]any{"posts": items})
		return
	}
	writeJSON(w, 405, map[string]any{"ok": false})
}

func (a *appServer) adminPostAction(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/posts/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || r.Method != http.MethodPost {
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id < 1 {
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}

	if parts[1] == "edit" {
		var req struct {
			Title      string `json:"title"`
			Body       string `json:"body"`
			Visibility string `json:"visibility"`
			DivisionID int64  `json:"division_id"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "The post could not be read."})
			return
		}
		req.Title = strings.TrimSpace(req.Title)
		req.Body = strings.TrimSpace(req.Body)
		req.Visibility = strings.ToLower(strings.TrimSpace(req.Visibility))
		if req.Body == "" || (req.Visibility != "public" && req.Visibility != "organization" && req.Visibility != "division") {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "Post text and a valid audience are required."})
			return
		}
		if req.Visibility == "division" && req.DivisionID < 1 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "Choose a division for this post."})
			return
		}
		_, err = a.db.Pool().Exec(r.Context(), `UPDATE posts SET title=NULLIF($1,''),body=$2,visibility=$3,division_id=CASE WHEN $3='division' THEN $4 ELSE NULL END,updated_at=NOW() WHERE id=$5`, req.Title, req.Body, req.Visibility, req.DivisionID, id)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "The post could not be updated."})
			return
		}
		_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO audit_logs(actor_account_id,action,entity_type,entity_id) VALUES($1,'post.edit','post',$2)`, actor.AccountID, fmt.Sprint(id))
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}

	if parts[1] == "delete" {
		tx, err := a.db.Pool().Begin(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		defer tx.Rollback(r.Context())
		var payload, postLabel string
		if tx.QueryRow(r.Context(), `SELECT to_jsonb(p)::text,COALESCE(NULLIF(p.title,''),NULLIF(LEFT(p.body,80),''),'Post') FROM posts p WHERE p.id=$1`, id).Scan(&payload, &postLabel) != nil {
			writeJSON(w, 404, map[string]any{"ok": false, "error": "Post not found."})
			return
		}
		_, _ = tx.Exec(r.Context(), `INSERT INTO archive_items(entity_type,entity_id,label,payload,archived_by) VALUES('post',$1,$2,$3::jsonb,$4)`, fmt.Sprint(id), postLabel, payload, actor.AccountID)
		if _, err = tx.Exec(r.Context(), `UPDATE posts SET moderation_status='hidden',updated_at=NOW() WHERE id=$1`, id); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		_, _ = tx.Exec(r.Context(), `INSERT INTO audit_logs(actor_account_id,action,entity_type,entity_id) VALUES($1,'post.archive','post',$2)`, actor.AccountID, fmt.Sprint(id))
		if err = tx.Commit(r.Context()); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}

	status := ""
	switch parts[1] {
	case "publish":
		status = "published"
	case "hide":
		status = "hidden"
	case "reject":
		status = "rejected"
	default:
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}
	_, err = a.db.Pool().Exec(r.Context(), `UPDATE posts SET moderation_status=$1,published_at=CASE WHEN $1='published' THEN COALESCE(published_at,NOW()) ELSE published_at END,updated_at=NOW() WHERE id=$2`, status, id)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	var authorID int64
	var title string
	if a.db.Pool().QueryRow(r.Context(), `SELECT author_account_id,COALESCE(NULLIF(title,''),'Your post') FROM posts WHERE id=$1`, id).Scan(&authorID, &title) == nil {
		noteTitle := "Post status updated"
		noteBody := title + " is now " + status + "."
		if status == "published" {
			noteTitle = "Public post approved"
			noteBody = title + " is now visible on the public AITS page."
		}
		_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO notifications(account_id,notification_type,title,body,action_url,target_post_id) SELECT $1,'post',$2,$3,$4,$5 WHERE EXISTS(SELECT 1 FROM member_profiles WHERE account_id=$1)`, authorID, noteTitle, noteBody, fmt.Sprintf("/member/posts/%d", id), id)
		a.notifyMemberNotification(authorID)
		if status == "published" {
			rows, notifyErr := a.db.Pool().Query(r.Context(), `INSERT INTO notifications(account_id,notification_type,title,body,action_url,target_post_id) SELECT mp.account_id,'post','New public post',$1,$2,$3 FROM member_profiles mp JOIN accounts ac ON ac.id=mp.account_id WHERE mp.account_id<>$4 AND ac.status='active' RETURNING mp.account_id`, title, fmt.Sprintf("/member/posts/%d", id), id, authorID)
			if notifyErr == nil {
				for rows.Next() {
					var target int64
					if rows.Scan(&target) == nil {
						a.notifyMemberNotification(target)
					}
				}
				rows.Close()
			}
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *appServer) adminAdministration(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		a.adminAdministrationList(w, r)
	case http.MethodPost:
		a.adminAdministrationSave(w, r)
	default:
		writeJSON(w, 405, map[string]any{"ok": false})
	}
}
func (a *appServer) adminAdministrationList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := a.db.Pool().Query(ctx, `SELECT am.id,am.account_id,ac.display_name,COALESCE(ac.username,''),COALESCE(ac.avatar_url,''),am.sort_order,am.is_published,ARRAY(SELECT amp.position_title FROM administration_member_positions amp WHERE amp.administration_member_id=am.id ORDER BY amp.sort_order,amp.position_title) FROM administration_members am JOIN accounts ac ON ac.id=am.account_id ORDER BY am.sort_order,am.id`)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer rows.Close()
	directory := []map[string]any{}
	for rows.Next() {
		var id, accountID int64
		var name, username, avatar string
		var sort int
		var published bool
		var positions []string
		if rows.Scan(&id, &accountID, &name, &username, &avatar, &sort, &published, &positions) == nil {
			directory = append(directory, map[string]any{"id": id, "account_id": accountID, "name": name, "username": username, "avatar": avatar, "positions": positions, "sort_order": sort, "published": published})
		}
	}
	writeJSON(w, 200, map[string]any{"administration": directory})
}
func nullableDate(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
func (a *appServer) adminAdministrationSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        int64    `json:"id"`
		AccountID int64    `json:"account_id"`
		Positions []string `json:"positions"`
		SortOrder int      `json:"sort_order"`
		Published bool     `json:"published"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 24<<10)).Decode(&req) != nil {
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}
	if req.AccountID < 1 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "choose a member"})
		return
	}
	positions := []string{}
	seen := map[string]bool{}
	for _, p := range req.Positions {
		p = strings.TrimSpace(p)
		key := strings.ToLower(p)
		if p != "" && len(p) <= 120 && !seen[key] {
			positions = append(positions, p)
			seen[key] = true
		}
	}
	if len(positions) == 0 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "add at least one position"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer tx.Rollback(ctx)
	var id int64
	if req.ID > 0 {
		err = tx.QueryRow(ctx, `UPDATE administration_members SET account_id=$1,category='officer',position_title=$2,sort_order=$3,is_published=$4,updated_at=NOW() WHERE id=$5 RETURNING id`, req.AccountID, positions[0], req.SortOrder, req.Published, req.ID).Scan(&id)
	} else {
		err = tx.QueryRow(ctx, `INSERT INTO administration_members(account_id,category,position_title,sort_order,is_published) VALUES($1,'officer',$2,$3,$4) RETURNING id`, req.AccountID, positions[0], req.SortOrder, req.Published).Scan(&id)
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "could not save administration card"})
		return
	}
	_, _ = tx.Exec(ctx, `DELETE FROM administration_member_positions WHERE administration_member_id=$1`, id)
	for i, p := range positions {
		if _, err = tx.Exec(ctx, `INSERT INTO administration_member_positions(administration_member_id,position_title,sort_order) VALUES($1,$2,$3)`, id, p, i); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
	}
	if tx.Commit(ctx) != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": id})
}

func (a *appServer) adminAdministrationItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	if r.Method != http.MethodDelete {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/administration/"), "/")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}
	_, err = a.db.Pool().Exec(r.Context(), `DELETE FROM administration_members WHERE id=$1`, id)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *appServer) adminRoles(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	var actorLevel int
	_ = a.db.Pool().QueryRow(r.Context(), `SELECT hierarchy_level FROM roles WHERE code=$1`, actor.Role).Scan(&actorLevel)
	rows, err := a.db.Pool().Query(r.Context(), `SELECT id,code,name,hierarchy_level,scope,is_administrator FROM roles ORDER BY hierarchy_level DESC,name`)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var code, name, scope string
		var level int
		var admin bool
		if rows.Scan(&id, &code, &name, &level, &scope, &admin) == nil {
			assignable := actor.Role == "developer" || level < actorLevel
			items = append(items, map[string]any{"id": id, "code": code, "name": name, "hierarchy_level": level, "scope": scope, "administrator": admin, "assignable": assignable})
		}
	}
	writeJSON(w, 200, map[string]any{"roles": items})
}

func (a *appServer) adminMemberItem(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/members/"), "/")
	parts := strings.Split(raw, "/")
	accountID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || accountID < 1 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid member"})
		return
	}
	if len(parts) == 2 && parts[1] == "password-reset" {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]any{"ok": false})
			return
		}
		a.adminMemberPasswordReset(w, r, actor, accountID)
		return
	}
	if len(parts) != 1 {
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	if r.Method != http.MethodPatch {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	var req struct {
		DivisionID int64    `json:"division_id"`
		Membership string   `json:"membership"`
		RoleID     int64    `json:"role_id"`
		Tags       []string `json:"tags"`
		Email      *string  `json:"email"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 24<<10)).Decode(&req) != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid request"})
		return
	}
	req.Membership = strings.TrimSpace(req.Membership)
	if req.Membership == "" || len(req.Membership) > 40 || req.RoleID < 1 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "membership and access are required"})
		return
	}
	var actorLevel, targetCurrentLevel, requestedLevel int
	var requestedCode, requestedScope, targetCurrentCode string
	_ = a.db.Pool().QueryRow(r.Context(), `SELECT hierarchy_level FROM roles WHERE code=$1`, actor.Role).Scan(&actorLevel)
	_ = a.db.Pool().QueryRow(r.Context(), `SELECT COALESCE(r.hierarchy_level,0),COALESCE(r.code,'member') FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE ar.account_id=$1 AND ar.is_active=TRUE ORDER BY r.hierarchy_level DESC LIMIT 1`, accountID).Scan(&targetCurrentLevel, &targetCurrentCode)
	if targetCurrentCode == "developer" {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "developer accounts are protected"})
		return
	}
	var requestedEmail string
	if req.Email != nil {
		if actor.Role != "developer" {
			writeJSON(w, 403, map[string]any{"ok": false, "error": "only a developer can add or change a member email"})
			return
		}
		requestedEmail = strings.TrimSpace(*req.Email)
		if !validEmailAddress(requestedEmail) {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "enter a valid member email"})
			return
		}
		var emailExists bool
		if err := a.db.Pool().QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM accounts WHERE LOWER(COALESCE(email,''))=LOWER($1) AND id<>$2)`, requestedEmail, accountID).Scan(&emailExists); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "member email could not be checked"})
			return
		}
		if emailExists {
			writeJSON(w, 409, map[string]any{"ok": false, "error": "that email is already assigned to another account"})
			return
		}
	}
	if err := a.db.Pool().QueryRow(r.Context(), `SELECT code,hierarchy_level,scope FROM roles WHERE id=$1`, req.RoleID).Scan(&requestedCode, &requestedLevel, &requestedScope); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "selected access level is unavailable"})
		return
	}
	if actor.Role != "developer" && (requestedLevel >= actorLevel || targetCurrentLevel >= actorLevel) {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "you cannot assign or change an equal or higher access level"})
		return
	}
	if requestedCode == "developer" && actor.Role != "developer" {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "developer access can only be assigned by a developer"})
		return
	}
	if requestedScope == "division" && req.DivisionID < 1 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "this access level requires a division"})
		return
	}
	if req.DivisionID > 0 {
		var valid bool
		_ = a.db.Pool().QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM divisions WHERE id=$1 AND is_active=TRUE)`, req.DivisionID).Scan(&valid)
		if !valid {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "selected division is unavailable"})
			return
		}
	}
	cleanTags := make([]string, 0, len(req.Tags))
	seen := map[string]bool{}
	for _, tag := range req.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || len(tag) > 60 || seen[strings.ToLower(tag)] {
			continue
		}
		seen[strings.ToLower(tag)] = true
		cleanTags = append(cleanTags, tag)
		if len(cleanTags) >= 8 {
			break
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 7*time.Second)
	defer cancel()
	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer tx.Rollback(ctx)
	if req.Email != nil {
		if _, err = tx.Exec(ctx, `UPDATE accounts SET email=$1,updated_at=NOW() WHERE id=$2`, requestedEmail, accountID); err != nil {
			writeJSON(w, 409, map[string]any{"ok": false, "error": "member email could not be updated; it may already be in use"})
			return
		}
	}
	if req.DivisionID > 0 {
		_, err = tx.Exec(ctx, `UPDATE member_profiles SET primary_division_id=$1,membership_level=$2,updated_at=NOW() WHERE account_id=$3`, req.DivisionID, req.Membership, accountID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE member_profiles SET primary_division_id=NULL,membership_level=$1,updated_at=NOW() WHERE account_id=$2`, req.Membership, accountID)
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "member record could not be updated"})
		return
	}
	_, _ = tx.Exec(ctx, `UPDATE account_roles SET is_active=FALSE,updated_at=NOW() WHERE account_id=$1 AND is_active=TRUE`, accountID)
	var roleDivision any
	var roleScope string
	_ = tx.QueryRow(ctx, `SELECT scope FROM roles WHERE id=$1`, req.RoleID).Scan(&roleScope)
	if roleScope == "division" && req.DivisionID > 0 {
		roleDivision = req.DivisionID
	}
	_, err = tx.Exec(ctx, `INSERT INTO account_roles(account_id,role_id,division_id,granted_by,is_active) VALUES($1,$2,$3,$4,TRUE) ON CONFLICT DO NOTHING`, accountID, req.RoleID, roleDivision, actor.AccountID)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE account_roles SET granted_by=$4,is_active=TRUE,starts_at=NOW(),expires_at=NULL,updated_at=NOW() WHERE account_id=$1 AND role_id=$2 AND COALESCE(division_id,0)=COALESCE($3::bigint,0)`, accountID, req.RoleID, roleDivision, actor.AccountID)
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "access could not be updated"})
		return
	}
	_, _ = tx.Exec(ctx, `DELETE FROM member_tags WHERE account_id=$1`, accountID)
	for i, tag := range cleanTags {
		if _, err = tx.Exec(ctx, `INSERT INTO member_tags(account_id,tag,sort_order) VALUES($1,$2,$3)`, accountID, tag, i); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "tags could not be updated"})
			return
		}
	}
	_, _ = tx.Exec(ctx, `INSERT INTO notifications(account_id,notification_type,title,body) VALUES($1,'account','Membership record updated','Your AITS division, access, membership, or profile tags were updated by an administrator.')`, accountID)
	if err = tx.Commit(ctx); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	a.refreshMemberSessionsForAccount(ctx, accountID)
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Member record updated."})
}

func (a *appServer) adminMemberPasswordReset(w http.ResponseWriter, r *http.Request, actor memberSessionEntry, accountID int64) {
	if !a.config.SMTPReady() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "email service is not available"})
		return
	}
	var actorLevel int
	if err := a.db.Pool().QueryRow(r.Context(), `SELECT hierarchy_level FROM roles WHERE code=$1`, actor.Role).Scan(&actorLevel); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "administrator access could not be verified"})
		return
	}
	var name, email, roleCode, status string
	var targetLevel int
	err := a.db.Pool().QueryRow(r.Context(), `
		SELECT a.display_name,COALESCE(a.email,''),a.status,
		       COALESCE((SELECT r.code FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE ar.account_id=a.id AND ar.is_active=TRUE ORDER BY r.hierarchy_level DESC LIMIT 1),'member'),
		       COALESCE((SELECT r.hierarchy_level FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE ar.account_id=a.id AND ar.is_active=TRUE ORDER BY r.hierarchy_level DESC LIMIT 1),0)
		FROM accounts a WHERE a.id=$1 AND a.account_kind='member'`, accountID).Scan(&name, &email, &status, &roleCode, &targetLevel)
	if err != nil || status != "active" {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "member account is unavailable"})
		return
	}
	if roleCode == "developer" || targetLevel >= actorLevel {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "password resets can only be sent to a lower-access member"})
		return
	}
	if !validEmailAddress(email) {
		writeJSON(w, 409, map[string]any{"ok": false, "error": "this member has no usable email. A developer must add one first."})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer tx.Rollback(ctx)
	// Expired links are made inert before checking for a currently active link.
	_, _ = tx.Exec(ctx, `UPDATE admin_password_reset_links SET used_at=COALESCE(used_at,NOW()) WHERE account_id=$1 AND used_at IS NULL AND expires_at<=NOW()`, accountID)
	var active bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_password_reset_links WHERE account_id=$1 AND used_at IS NULL AND expires_at>NOW())`, accountID).Scan(&active); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	if active {
		writeJSON(w, 409, map[string]any{"ok": false, "error": "this member already has an active password reset link. It must be used or expire first."})
		return
	}
	token, err := randomToken(40)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	tokenHash := activationTokenHash(token)
	if _, err = tx.Exec(ctx, `INSERT INTO admin_password_reset_links(account_id,requested_by,token_hash,expires_at) VALUES($1,$2,$3,NOW()+INTERVAL '15 minutes')`, accountID, actor.AccountID, tokenHash); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "password reset link could not be created"})
		return
	}
	if err = tx.Commit(ctx); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	resetURL := strings.TrimRight(a.config.PublicURL, "/") + "/reset-password?token=" + token
	if err = a.mailer.SendPasswordReset(email, name, resetURL); err != nil {
		_, _ = a.db.Pool().Exec(ctx, `DELETE FROM admin_password_reset_links WHERE token_hash=$1 AND used_at IS NULL`, tokenHash)
		writeJSON(w, 503, map[string]any{"ok": false, "error": "password reset email could not be sent"})
		return
	}
	_, _ = a.db.Pool().Exec(ctx, `INSERT INTO audit_logs(actor_account_id,action,entity_type,entity_id,details) VALUES($1,'member.password_reset_email','member',$2,jsonb_build_object('email',$3))`, actor.AccountID, strconv.FormatInt(accountID, 10), email)
	writeJSON(w, 200, map[string]any{"ok": true, "message": "A one-time password reset link was sent to the member email.", "expires_in_seconds": 900})
}

// memberAdminPasswordReset is intentionally public: possession of the random,
// single-use reset token is the authorization for this narrow operation.
func (a *appServer) memberAdminPasswordReset(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if r.Method == http.MethodPost {
		var req struct {
			Token       string `json:"token"`
			NewPassword string `json:"new_password"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid reset request"})
			return
		}
		token = strings.TrimSpace(req.Token)
		if token == "" || len(req.NewPassword) < 10 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "use a valid reset link and a password with at least 10 characters"})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
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
		var resetID, accountID int64
		err = tx.QueryRow(r.Context(), `SELECT pr.id,pr.account_id FROM admin_password_reset_links pr JOIN accounts a ON a.id=pr.account_id WHERE pr.token_hash=$1 AND pr.used_at IS NULL AND pr.expires_at>NOW() AND a.status='active' FOR UPDATE OF pr`, activationTokenHash(token)).Scan(&resetID, &accountID)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "this password reset link is invalid, expired, or already used"})
			return
		}
		if _, err = tx.Exec(r.Context(), `UPDATE accounts SET password_hash=$1,updated_at=NOW() WHERE id=$2`, string(hash), accountID); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "password could not be updated"})
			return
		}
		if _, err = tx.Exec(r.Context(), `UPDATE admin_password_reset_links SET used_at=NOW() WHERE id=$1 AND used_at IS NULL`, resetID); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		a.invalidateMemberSessions(r.Context(), accountID, "")
		writeJSON(w, 200, map[string]any{"ok": true, "message": "Password updated. You can sign in with your new password."})
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	if token == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "password reset token is missing"})
		return
	}
	var name string
	var expires time.Time
	err := a.db.Pool().QueryRow(r.Context(), `SELECT a.display_name,pr.expires_at FROM admin_password_reset_links pr JOIN accounts a ON a.id=pr.account_id WHERE pr.token_hash=$1 AND pr.used_at IS NULL AND pr.expires_at>NOW() AND a.status='active' LIMIT 1`, activationTokenHash(token)).Scan(&name, &expires)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "this password reset link is invalid, expired, or already used"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "valid": true, "name": name, "expires_at": expires.UTC().Format(time.RFC3339)})
}

func (a *appServer) adminMemberRequests(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	rows, err := a.db.Pool().Query(r.Context(), `SELECT cr.id,cr.account_id,a.display_name,COALESCE(a.username,''),cr.request_type,COALESCE(d.name,''),COALESCE(cr.requested_division_id,0),COALESCE(cr.requested_membership,''),cr.status,cr.created_at FROM member_change_requests cr JOIN accounts a ON a.id=cr.account_id LEFT JOIN divisions d ON d.id=cr.requested_division_id WHERE cr.status='pending' ORDER BY cr.created_at ASC`)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, accountID, divisionID int64
		var name, username, typ, division, membership, status string
		var created time.Time
		if rows.Scan(&id, &accountID, &name, &username, &typ, &division, &divisionID, &membership, &status, &created) == nil {
			items = append(items, map[string]any{"id": id, "account_id": accountID, "name": name, "username": username, "type": typ, "division": division, "division_id": divisionID, "membership": membership, "status": status, "time": formatPublicDateTime(created)})
		}
	}
	writeJSON(w, 200, map[string]any{"requests": items})
}

func (a *appServer) adminMemberRequestAction(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/member-requests/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || (parts[1] != "approve" && parts[1] != "decline") {
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer tx.Rollback(ctx)
	var accountID, divisionID int64
	var status, typ, membership string
	if err = tx.QueryRow(ctx, `SELECT account_id,COALESCE(requested_division_id,0),status,request_type,COALESCE(requested_membership,'') FROM member_change_requests WHERE id=$1 FOR UPDATE`, id).Scan(&accountID, &divisionID, &status, &typ, &membership); err != nil || status != "pending" {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "request is no longer pending"})
		return
	}
	newStatus := "declined"
	title := "Request declined"
	body := "Your requested account change was not approved."
	if parts[1] == "approve" {
		switch typ {
		case "division":
			if divisionID < 1 {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "division request is incomplete"})
				return
			}
			_, err = tx.Exec(ctx, `UPDATE member_profiles SET primary_division_id=$1,updated_at=NOW() WHERE account_id=$2`, divisionID, accountID)
			title = "Division request approved"
			body = "Your requested AITS division is now active on your profile."
		case "membership":
			if strings.TrimSpace(membership) == "" {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "membership request is incomplete"})
				return
			}
			_, err = tx.Exec(ctx, `UPDATE member_profiles SET membership_level=$1,updated_at=NOW() WHERE account_id=$2`, membership, accountID)
			title = "Membership request approved"
			body = "Your membership classification has been updated."
		default:
			err = fmt.Errorf("unsupported request")
		}
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		newStatus = "approved"
	}
	_, err = tx.Exec(ctx, `UPDATE member_change_requests SET status=$1,reviewed_by=$2,reviewed_at=NOW(),updated_at=NOW() WHERE id=$3`, newStatus, actor.AccountID, id)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	if newStatus == "declined" {
		if typ == "division" {
			title = "Division request declined"
		} else if typ == "membership" {
			title = "Membership request declined"
		}
	}
	_, _ = tx.Exec(ctx, `INSERT INTO notifications(account_id,notification_type,title,body) VALUES($1,'account',$2,$3)`, accountID, title, body)
	if tx.Commit(ctx) != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	a.refreshMemberSessionsForAccount(ctx, accountID)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *appServer) adminApplications(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	rows, err := a.db.Pool().Query(r.Context(), `SELECT ap.id,ap.account_id,COALESCE(ap.target_division_id,0),ap.application_number,a.display_name,COALESCE(a.username,''),COALESCE(a.email,''),ap.assessment_mode,COALESCE(d.name,''),ap.status,COALESCE(ap.overall_score,0),COALESCE(ap.response_score,0),COALESCE(ap.submitted_at,ap.created_at) FROM applications ap JOIN accounts a ON a.id=ap.account_id LEFT JOIN divisions d ON d.id=ap.target_division_id WHERE NOT EXISTS (SELECT 1 FROM archive_items ai WHERE ai.entity_type='application' AND ai.entity_id=ap.id::text AND ai.restored_at IS NULL AND ai.permanently_deleted_at IS NULL AND ai.expires_at>NOW()) ORDER BY COALESCE(ap.submitted_at,ap.created_at) DESC LIMIT 200`)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, accountID, divisionID int64
		var number, name, username, email, mode, division, status string
		var overall, response float64
		var created time.Time
		if rows.Scan(&id, &accountID, &divisionID, &number, &name, &username, &email, &mode, &division, &status, &overall, &response, &created) == nil {
			items = append(items, map[string]any{"id": id, "account_id": accountID, "division_id": divisionID, "application_number": number, "name": name, "username": username, "email": email, "mode": mode, "division": division, "status": status, "overall": overall, "response": response, "time": formatPublicDateTime(created)})
		}
	}
	writeJSON(w, 200, map[string]any{"applications": items})
}

func (a *appServer) adminAssessments(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	var questions, attempts, completed int64
	_ = a.db.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM assessment_questions WHERE is_active=TRUE`).Scan(&questions)
	_ = a.db.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM assessment_attempts`).Scan(&attempts)
	_ = a.db.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM assessment_attempts WHERE status='completed'`).Scan(&completed)
	rows, err := a.db.Pool().Query(ctx, `SELECT COALESCE(d.name,'General'),COUNT(q.id) FROM assessment_questions q LEFT JOIN divisions d ON d.id=q.division_id WHERE q.is_active=TRUE GROUP BY d.name,q.category ORDER BY COALESCE(d.name,'General')`)
	byCategory := make([]map[string]any, 0)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var label string
			var count int64
			if rows.Scan(&label, &count) == nil {
				byCategory = append(byCategory, map[string]any{"label": label, "count": count})
			}
		}
	}
	writeJSON(w, 200, map[string]any{"summary": map[string]any{"questions": questions, "attempts": attempts, "completed": completed}, "question_groups": byCategory})
}

func (a *appServer) adminLibrary(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		rows, err := a.db.Pool().Query(r.Context(), `SELECT ml.id,ml.label,ml.media_type,ml.url,COALESCE(ml.extension,''),COALESCE(ml.location_label,''),COALESCE(ml.reason,''),COALESCE(ml.file_size,0),COALESCE(ml.source_type,'path'),ml.is_active,ml.created_at,COALESCE(a.display_name,'System'),(SELECT COUNT(*) FROM media_library_usage u WHERE u.media_id=ml.id),ml.visibility,COALESCE(d.id,0),COALESCE(d.name,''),COALESCE(d.code,''),COALESCE(d.logo_url,'') FROM media_library ml LEFT JOIN accounts a ON a.id=ml.uploaded_by LEFT JOIN divisions d ON d.id=ml.division_id ORDER BY ml.created_at DESC,ml.id DESC`)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, size, uses, divisionID int64
			var label, typ, url, ext, location, reason, source, who, visibility, division, divisionCode, divisionLogo string
			var active bool
			var created time.Time
			if rows.Scan(&id, &label, &typ, &url, &ext, &location, &reason, &size, &source, &active, &created, &who, &uses, &visibility, &divisionID, &division, &divisionCode, &divisionLogo) == nil {
				refs := a.libraryReferences(r.Context(), id, url)
				items = append(items, map[string]any{"id": id, "label": label, "type": typ, "url": url, "extension": ext, "location": location, "reason": reason, "size": size, "source": source, "active": active, "time": formatPublicDateTime(created), "added_by": who, "usage_count": uses, "reference_count": len(refs), "references": refs, "visibility": visibility, "division_id": divisionID, "division": division, "division_code": divisionCode, "division_logo": divisionLogo})
			}
		}
		writeJSON(w, 200, map[string]any{"items": items})
	case http.MethodPost:
		var req struct {
			ID                                 int64 `json:"id"`
			Label, Type, URL, Location, Reason string
			Active                             bool   `json:"active"`
			Visibility                         string `json:"visibility"`
			DivisionID                         int64  `json:"division_id"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 24<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false})
			return
		}
		req.Label = strings.TrimSpace(req.Label)
		req.URL = strings.TrimSpace(req.URL)
		req.Type = strings.TrimSpace(req.Type)
		if req.Label == "" || req.URL == "" {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "label and URL are required"})
			return
		}
		if req.Type == "" {
			req.Type = "link"
		}
		req.Visibility = strings.ToLower(strings.TrimSpace(req.Visibility))
		if req.Visibility == "" {
			req.Visibility = "organization"
		}
		if req.Visibility != "public" && req.Visibility != "organization" && req.Visibility != "division" {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid library visibility"})
			return
		}
		if req.Visibility == "division" && req.DivisionID < 1 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "choose a division for division-only media"})
			return
		}
		ext := ""
		if i := strings.LastIndex(req.URL, "."); i >= 0 && len(req.URL)-i <= 10 {
			ext = strings.Trim(strings.ToLower(req.URL[i+1:]), "/?#")
		}
		if req.ID > 0 {
			_, err := a.db.Pool().Exec(r.Context(), `UPDATE media_library SET label=$1,media_type=$2,url=$3,extension=NULLIF($4,''),location_label=NULLIF($5,''),reason=NULLIF($6,''),is_active=$7,visibility=$8,division_id=NULLIF($9,0),updated_at=NOW() WHERE id=$10`, req.Label, req.Type, req.URL, ext, strings.TrimSpace(req.Location), strings.TrimSpace(req.Reason), req.Active, req.Visibility, req.DivisionID, req.ID)
			if err != nil {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "library item could not be updated"})
				return
			}
		} else {
			var newID int64
			err := a.db.Pool().QueryRow(r.Context(), `INSERT INTO media_library(label,media_type,url,visibility,division_id,uploaded_by,is_active,extension,location_label,reason,source_type) VALUES($1,$2,$3,$4,NULLIF($5,0),$6,$7,NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),'url') RETURNING id`, req.Label, req.Type, req.URL, req.Visibility, req.DivisionID, actor.AccountID, req.Active, ext, strings.TrimSpace(req.Location), strings.TrimSpace(req.Reason)).Scan(&newID)
			if err != nil {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "library item could not be added"})
				return
			}
			_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO media_library_usage(media_id,account_id,location_label,action,reason) VALUES($1,$2,'Admin · Library','registered',$3)`, newID, actor.AccountID, firstNonEmpty(strings.TrimSpace(req.Reason), "Registered in the managed Library."))
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		writeJSON(w, 405, map[string]any{"ok": false})
	}
}

func (a *appServer) adminLibraryItem(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodDelete {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/library/"), "/")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}
	var url string
	if a.db.Pool().QueryRow(r.Context(), `SELECT url FROM media_library WHERE id=$1`, id).Scan(&url) != nil {
		requestedURL := strings.TrimSpace(r.URL.Query().Get("url"))
		if requestedURL != "" {
			var resolvedID int64
			if a.db.Pool().QueryRow(r.Context(), `SELECT id,url FROM media_library WHERE url=$1 ORDER BY id DESC LIMIT 1`, requestedURL).Scan(&resolvedID, &url) == nil {
				id = resolvedID
			}
		}
		if url == "" {
			var archived bool
			_ = a.db.Pool().QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM archive_items WHERE entity_type='library' AND entity_id=$1::text AND restored_at IS NULL AND permanently_deleted_at IS NULL)`, id).Scan(&archived)
			if archived {
				writeJSON(w, 200, map[string]any{"ok": true, "already_archived": true})
				return
			}
			writeJSON(w, 404, map[string]any{"ok": false, "error": "library item was not found; Library has been refreshed"})
			return
		}
	}
	refs := a.libraryReferences(r.Context(), id, url)
	if len(refs) > 0 {
		writeJSON(w, 409, map[string]any{"ok": false, "error": "This Library item is still linked to active content. Remove those references first.", "references": refs})
		return
	}
	if err := a.archiveLibraryItem(r.Context(), actor.AccountID, id); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "library item could not be archived"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func encryptInvitePassword(token, password string) (string, error) {
	key := sha256.Sum256([]byte(token))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = crand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(password), nil)
	return base64.RawURLEncoding.EncodeToString(append(nonce, sealed...)), nil
}

func decryptInvitePassword(token, encoded string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(token))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid invitation secret")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (a *appServer) adminCreateMember(w http.ResponseWriter, r *http.Request, actor memberSessionEntry) {
	var req struct {
		ApplicationID int64  `json:"application_id"`
		Name          string `json:"name"`
		Username      string `json:"username"`
		Email         string `json:"email"`
		Password      string `json:"password"`
		DivisionID    int64  `json:"division_id"`
		Membership    string `json:"membership"`
		RoleID        int64  `json:"role_id"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&req) != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid member details"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Membership = strings.TrimSpace(req.Membership)
	if req.Name == "" || !memberUsernamePattern.MatchString(req.Username) || len(req.Password) < 10 || !strings.Contains(req.Email, "@") {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "full name, email, valid username, and a temporary password with at least 10 characters are required"})
		return
	}
	if req.Membership != "Member" && req.Membership != "Core Member" && req.Membership != "Officer" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "membership must be Member, Core Member, or Officer"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var actorLevel int
	_ = a.db.Pool().QueryRow(ctx, `SELECT COALESCE(MAX(r.hierarchy_level),0) FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE ar.account_id=$1 AND ar.is_active=TRUE`, actor.AccountID).Scan(&actorLevel)
	if req.RoleID == 0 {
		_ = a.db.Pool().QueryRow(ctx, `SELECT id FROM roles WHERE code='member' LIMIT 1`).Scan(&req.RoleID)
	}
	var roleLevel int
	var roleCode, roleScope string
	if a.db.Pool().QueryRow(ctx, `SELECT hierarchy_level,code,scope FROM roles WHERE id=$1`, req.RoleID).Scan(&roleLevel, &roleCode, &roleScope) != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "select a valid access level"})
		return
	}
	if roleCode == "developer" || roleLevel >= actorLevel {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "you cannot invite a member with an access level equal to or above your own"})
		return
	}
	if roleScope == "division" && req.DivisionID < 1 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "this access level requires a division"})
		return
	}
	if req.DivisionID > 0 {
		var valid bool
		_ = a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM divisions WHERE id=$1 AND is_active=TRUE)`, req.DivisionID).Scan(&valid)
		if !valid {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "selected division is unavailable"})
			return
		}
	}
	var exists bool
	_ = a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE LOWER(COALESCE(email,''))=LOWER($1) OR LOWER(COALESCE(username,''))=LOWER($2))`, req.Email, req.Username).Scan(&exists)
	if exists {
		writeJSON(w, 409, map[string]any{"ok": false, "error": "that email or username already belongs to an account"})
		return
	}
	_ = a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member_activation_invites WHERE used_at IS NULL AND expires_at>NOW() AND invite_kind='direct' AND (LOWER(COALESCE(invite_email,''))=LOWER($1) OR LOWER(COALESCE(invite_username,''))=LOWER($2)))`, req.Email, req.Username).Scan(&exists)
	if exists {
		writeJSON(w, 409, map[string]any{"ok": false, "error": "an active 48-hour invitation already exists for that email or username"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	token, err := randomToken(40)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	ciphertext, err := encryptInvitePassword(token, req.Password)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	var division any = nil
	if req.DivisionID > 0 {
		division = req.DivisionID
	}
	_, err = a.db.Pool().Exec(ctx, `INSERT INTO member_activation_invites(application_id,account_id,token_hash,division_id,role_id,membership,created_by,expires_at,invite_kind,invite_name,invite_email,invite_username,invite_password_hash,invite_password_cipher) VALUES(NULL,NULL,$1,$2,$3,$4,$5,NOW()+INTERVAL '48 hours','direct',$6,$7,$8,$9,$10)`, activationTokenHash(token), division, req.RoleID, req.Membership, actor.AccountID, req.Name, req.Email, req.Username, string(hash), ciphertext)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "invitation could not be created"})
		return
	}
	inviteURL := strings.TrimRight(a.config.PublicURL, "/") + "/activate-member?token=" + token
	if err = a.mailer.SendMemberInvitation(req.Email, req.Name, inviteURL); err != nil {
		_, _ = a.db.Pool().Exec(ctx, `DELETE FROM member_activation_invites WHERE token_hash=$1 AND used_at IS NULL`, activationTokenHash(token))
		writeJSON(w, 503, map[string]any{"ok": false, "error": "invitation email could not be sent"})
		return
	}
	_, _ = a.db.Pool().Exec(ctx, `INSERT INTO audit_logs(actor_account_id,action,entity_type,entity_id,details) VALUES($1,'member.invite','member_invite',$2,jsonb_build_object('email',$3,'username',$4))`, actor.AccountID, activationTokenHash(token), req.Email, req.Username)
	writeJSON(w, 201, map[string]any{"ok": true, "message": "48-hour member invitation sent.", "expires_in_hours": 48})
}

func activationTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func (a *appServer) adminApplicationItem(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/applications/"), "/")
	parts := strings.Split(raw, "/")
	id, _ := strconv.ParseInt(parts[0], 10, 64)
	if id < 1 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid application"})
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		var base map[string]any = map[string]any{}
		var number, name, email, mode, division, status, year string
		var overall, response float64
		var created time.Time
		err := a.db.Pool().QueryRow(r.Context(), `SELECT ap.application_number,a.display_name,COALESCE(a.email,''),ap.assessment_mode,COALESCE(d.name,''),ap.status,COALESCE(ap.year_level,''),COALESCE(ap.overall_score,0),COALESCE(ap.response_score,0),COALESCE(ap.submitted_at,ap.created_at) FROM applications ap JOIN accounts a ON a.id=ap.account_id LEFT JOIN divisions d ON d.id=ap.target_division_id WHERE ap.id=$1`, id).Scan(&number, &name, &email, &mode, &division, &status, &year, &overall, &response, &created)
		if err != nil {
			writeJSON(w, 404, map[string]any{"ok": false, "error": "application not found"})
			return
		}
		base = map[string]any{"id": id, "application_number": number, "name": name, "email": email, "mode": mode, "division": division, "status": status, "year_level": year, "overall": overall, "response": response, "time": formatPublicDateTime(created)}
		rows, err := a.db.Pool().Query(r.Context(), `SELECT ai.position,q.question_text,COALESCE(NULLIF(ai.selected_answer_text,''),c.answer_text,''),COALESCE((SELECT cc.answer_text FROM assessment_choices cc WHERE cc.question_id=q.id AND cc.is_correct=TRUE ORDER BY cc.id LIMIT 1),q.correct_answer,''),COALESCE(ai.is_correct,FALSE),ai.timed_out FROM assessment_attempts at JOIN assessment_attempt_items ai ON ai.attempt_id=at.id JOIN assessment_questions q ON q.id=ai.question_id LEFT JOIN assessment_choices c ON c.id=ai.selected_choice_id WHERE at.id=(SELECT id FROM assessment_attempts WHERE application_id=$1 ORDER BY id DESC LIMIT 1) ORDER BY ai.position`, id)
		items := []map[string]any{}
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var pos int
				var q, selected, correctAnswer string
				var correct, timed bool
				if rows.Scan(&pos, &q, &selected, &correctAnswer, &correct, &timed) == nil {
					items = append(items, map[string]any{"position": pos, "question": q, "selected": selected, "correct_answer": correctAnswer, "correct": correct, "timed_out": timed})
				}
			}
		}
		writeJSON(w, 200, map[string]any{"application": base, "answers": items})
		return
	}
	if len(parts) == 2 && parts[1] == "accept" && r.Method == http.MethodPost {
		var req struct {
			DivisionID int64  `json:"division_id"`
			RoleID     int64  `json:"role_id"`
			Membership string `json:"membership"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid acceptance details"})
			return
		}
		membership := strings.TrimSpace(req.Membership)
		if membership != "Member" && membership != "Core Member" && membership != "Officer" {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "select Member, Core Member, or Officer"})
			return
		}
		if req.DivisionID < 1 || req.RoleID < 1 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "division and access level are required"})
			return
		}
		var accountID int64
		var email, name string
		if a.db.Pool().QueryRow(r.Context(), `SELECT ap.account_id,COALESCE(ac.email,''),ac.display_name FROM applications ap JOIN accounts ac ON ac.id=ap.account_id WHERE ap.id=$1`, id).Scan(&accountID, &email, &name) != nil {
			writeJSON(w, 404, map[string]any{"ok": false, "error": "application not found"})
			return
		}
		if email == "" {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "applicant has no verified email"})
			return
		}
		var roleLevel int
		if a.db.Pool().QueryRow(r.Context(), `SELECT hierarchy_level FROM roles WHERE id=$1`, req.RoleID).Scan(&roleLevel) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid access level"})
			return
		}
		actorLevel, _ := adminRoleLevel(r.Context(), a, actor.AccountID)
		if actor.Role != "developer" && roleLevel >= actorLevel {
			writeJSON(w, 403, map[string]any{"ok": false, "error": "you cannot assign an access level equal to or above your own"})
			return
		}
		token, err := randomToken(40)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		hash := activationTokenHash(token)
		tx, err := a.db.Pool().Begin(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		defer tx.Rollback(r.Context())
		_, _ = tx.Exec(r.Context(), `UPDATE member_activation_invites SET used_at=COALESCE(used_at,NOW()),updated_at=NOW() WHERE application_id=$1 AND used_at IS NULL`, id)
		_, err = tx.Exec(r.Context(), `INSERT INTO member_activation_invites(application_id,account_id,token_hash,division_id,role_id,membership,created_by,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,NOW()+INTERVAL '48 hours')`, id, accountID, hash, req.DivisionID, req.RoleID, membership, actor.AccountID)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "invitation could not be created"})
			return
		}
		_, _ = tx.Exec(r.Context(), `UPDATE applications SET status='accepted',reviewed_at=NOW(),reviewed_by=$1,updated_at=NOW() WHERE id=$2`, actor.AccountID, id)
		if err = tx.Commit(r.Context()); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		url := strings.TrimRight(a.config.PublicURL, "/") + "/activate-member?token=" + token
		if err = a.mailer.SendMemberInvitation(email, name, url); err != nil {
			writeJSON(w, 503, map[string]any{"ok": false, "error": "application accepted, but the activation email could not be sent"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "message": "Acceptance email sent. The one-time link expires in 48 hours."})
		return
	}
	if len(parts) == 2 && parts[1] == "status" && r.Method == http.MethodPost {
		var req struct {
			Status string `json:"status"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid status update"})
			return
		}
		req.Status = strings.TrimSpace(req.Status)
		allowed := map[string]bool{"submitted": true, "for_review": true, "for_interview": true, "declined": true, "cancelled": true}
		if !allowed[req.Status] {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "unsupported application status"})
			return
		}
		result, err := a.db.Pool().Exec(r.Context(), `UPDATE applications SET status=$1,reviewed_at=CASE WHEN $1 IN ('for_review','for_interview','declined') THEN NOW() ELSE reviewed_at END,reviewed_by=CASE WHEN $1 IN ('for_review','for_interview','declined') THEN $2 ELSE reviewed_by END,updated_at=NOW() WHERE id=$3 AND status<>'accepted'`, req.Status, actor.AccountID, id)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "application status could not be updated"})
			return
		}
		if n := result.RowsAffected(); n == 0 {
			writeJSON(w, 409, map[string]any{"ok": false, "error": "accepted applications cannot be moved back into review"})
			return
		}
		_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO audit_logs(actor_account_id,action,entity_type,entity_id,details) VALUES($1,'application.status','application',$2,jsonb_build_object('status',$3))`, actor.AccountID, fmt.Sprint(id), req.Status)
		writeJSON(w, 200, map[string]any{"ok": true, "status": req.Status})
		return
	}
	if len(parts) == 2 && parts[1] == "archive" && r.Method == http.MethodPost {
		var label string
		var payload string
		err := a.db.Pool().QueryRow(r.Context(), `SELECT application_number,to_jsonb(ap)::text FROM applications ap WHERE id=$1`, id).Scan(&label, &payload)
		if err != nil {
			writeJSON(w, 404, map[string]any{"ok": false, "error": "application not found"})
			return
		}
		_, err = a.db.Pool().Exec(r.Context(), `INSERT INTO archive_items(entity_type,entity_id,label,payload,archived_by) SELECT 'application',$1,$2,$3::jsonb,$4 WHERE NOT EXISTS (SELECT 1 FROM archive_items WHERE entity_type='application' AND entity_id=$1 AND restored_at IS NULL AND permanently_deleted_at IS NULL)`, fmt.Sprint(id), label, payload, actor.AccountID)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "application could not be archived"})
			return
		}
		_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO audit_logs(actor_account_id,action,entity_type,entity_id) VALUES($1,'application.archive','application',$2)`, actor.AccountID, fmt.Sprint(id))
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	writeJSON(w, 405, map[string]any{"ok": false, "error": "requested application action is unavailable"})
}

func (a *appServer) applicationActivation(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if r.Method == http.MethodGet {
		if token == "" {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "activation token is required"})
			return
		}
		var kind, name, email, username, ciphertext string
		var expires time.Time
		err := a.db.Pool().QueryRow(r.Context(), `SELECT invite_kind,COALESCE(invite_name,ac.display_name,''),COALESCE(invite_email,ac.email,''),COALESCE(invite_username,''),COALESCE(invite_password_cipher,''),mi.expires_at FROM member_activation_invites mi LEFT JOIN accounts ac ON ac.id=mi.account_id WHERE mi.token_hash=$1 AND mi.used_at IS NULL AND mi.expires_at>NOW() ORDER BY mi.id DESC LIMIT 1`, activationTokenHash(token)).Scan(&kind, &name, &email, &username, &ciphertext, &expires)
		if err != nil {
			writeJSON(w, 410, map[string]any{"ok": false, "error": "this activation link is invalid, expired, or already used"})
			return
		}
		payload := map[string]any{"ok": true, "kind": kind, "name": name, "email": email, "expires_at": expires}
		if kind == "direct" {
			password, err := decryptInvitePassword(token, ciphertext)
			if err != nil {
				writeJSON(w, 410, map[string]any{"ok": false, "error": "invitation details could not be verified"})
				return
			}
			payload["username"] = username
			payload["temporary_password"] = password
		}
		writeJSON(w, 200, payload)
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	var req struct{ Token, Username, Password string }
	if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req) != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid activation request"})
		return
	}
	if token == "" {
		token = strings.TrimSpace(req.Token)
	}
	req.Username = strings.TrimSpace(req.Username)
	if token == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "activation token is required"})
		return
	}
	var inviteID, applicationID, accountID, divisionID, roleID int64
	var membership, kind, name, email, presetUsername, passwordHash string
	err := a.db.Pool().QueryRow(r.Context(), `SELECT id,COALESCE(application_id,0),COALESCE(account_id,0),COALESCE(division_id,0),role_id,membership,invite_kind,COALESCE(invite_name,''),COALESCE(invite_email,''),COALESCE(invite_username,''),COALESCE(invite_password_hash,'') FROM member_activation_invites WHERE token_hash=$1 AND used_at IS NULL AND expires_at>NOW() ORDER BY id DESC LIMIT 1`, activationTokenHash(token)).Scan(&inviteID, &applicationID, &accountID, &divisionID, &roleID, &membership, &kind, &name, &email, &presetUsername, &passwordHash)
	if err != nil {
		writeJSON(w, 410, map[string]any{"ok": false, "error": "this activation link is invalid, expired, or already used"})
		return
	}
	if kind == "direct" {
		if !memberUsernamePattern.MatchString(presetUsername) || passwordHash == "" {
			writeJSON(w, 410, map[string]any{"ok": false, "error": "invitation details are incomplete"})
			return
		}
		req.Username = presetUsername
	} else {
		if !memberUsernamePattern.MatchString(req.Username) || len(req.Password) < 10 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "use a valid username and a password with at least 10 characters"})
			return
		}
		h, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		passwordHash = string(h)
	}
	tx, err := a.db.Pool().Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer tx.Rollback(r.Context())
	if kind == "direct" {
		err = tx.QueryRow(r.Context(), `INSERT INTO accounts(username,email,password_hash,display_name,account_kind,status) VALUES($1,$2,$3,$4,'member','active') RETURNING id`, req.Username, email, passwordHash, name).Scan(&accountID)
		if err != nil {
			writeJSON(w, 409, map[string]any{"ok": false, "error": "that username or email is already in use"})
			return
		}
	} else {
		if _, err = tx.Exec(r.Context(), `UPDATE accounts SET username=$1,password_hash=$2,account_kind='member',status='active',updated_at=NOW() WHERE id=$3`, req.Username, passwordHash, accountID); err != nil {
			writeJSON(w, 409, map[string]any{"ok": false, "error": "that username is already in use"})
			return
		}
	}
	var department, course, year string
	var birth *time.Time
	if applicationID > 0 {
		_ = tx.QueryRow(r.Context(), `SELECT COALESCE(department,''),COALESCE(course,''),COALESCE(year_level,''),birth_date FROM applications WHERE id=$1`, applicationID).Scan(&department, &course, &year, &birth)
	}
	var division any = nil
	if divisionID > 0 {
		division = divisionID
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO member_profiles(account_id,member_number,department,course,year_level,primary_division_id,membership_level,birth_date,joined_at) VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),$6,$7,$8,CURRENT_DATE) ON CONFLICT(account_id) DO UPDATE SET department=EXCLUDED.department,course=EXCLUDED.course,year_level=EXCLUDED.year_level,primary_division_id=EXCLUDED.primary_division_id,membership_level=EXCLUDED.membership_level,birth_date=EXCLUDED.birth_date,joined_at=COALESCE(member_profiles.joined_at,CURRENT_DATE),updated_at=NOW()`, accountID, fmt.Sprintf("AITS-%06d", accountID), department, course, year, division, membership, birth)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "member profile could not be activated"})
		return
	}
	_, _ = tx.Exec(r.Context(), `UPDATE account_roles SET is_active=FALSE WHERE account_id=$1`, accountID)
	var roleScope string
	_ = tx.QueryRow(r.Context(), `SELECT scope FROM roles WHERE id=$1`, roleID).Scan(&roleScope)
	var roleDivision any = nil
	if roleScope == "division" && divisionID > 0 {
		roleDivision = divisionID
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO account_roles(account_id,role_id,division_id,is_active) VALUES($1,$2,$3,TRUE) ON CONFLICT DO NOTHING`, accountID, roleID, roleDivision)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "member access could not be activated"})
		return
	}
	_, _ = tx.Exec(r.Context(), `UPDATE member_activation_invites SET used_at=NOW(),invite_password_cipher=NULL,updated_at=NOW() WHERE id=$1`, inviteID)
	_, _ = tx.Exec(r.Context(), `INSERT INTO notifications(account_id,notification_type,title,body) VALUES($1,'account','Welcome to AITS','Your AITS member account is now active.')`, accountID)
	// Notify organization officers and the relevant division leadership when a new member becomes active.
	_, _ = tx.Exec(r.Context(), `INSERT INTO notifications(account_id,notification_type,title,body,action_url) SELECT DISTINCT mp.account_id,'member','New AITS member', $1 || ' has activated an AITS member account.','/member?tab=members' FROM member_profiles mp JOIN accounts ac ON ac.id=mp.account_id LEFT JOIN account_roles ar ON ar.account_id=mp.account_id AND ar.is_active=TRUE LEFT JOIN roles rr ON rr.id=ar.role_id WHERE mp.account_id<>$2 AND ac.status='active' AND (LOWER(COALESCE(mp.membership_level,''))='officer' OR LOWER(COALESCE(rr.name,'')) LIKE '%mayor%') AND ($3::bigint=0 OR mp.primary_division_id=$3 OR LOWER(COALESCE(mp.membership_level,''))='officer')`, firstNonEmpty(name, req.Username), accountID, divisionID)
	if err = tx.Commit(r.Context()); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Your member account is ready. You can sign in now."})
}
