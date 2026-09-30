package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type publicAnnouncement struct {
	ID          int64               `json:"id"`
	Type        string              `json:"type"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Date        string              `json:"date"`
	Status      string              `json:"status"`
	Action      *announcementAction `json:"action"`
}

type announcementAction struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}

type publicPost struct {
	ID           int64            `json:"id"`
	Pinned       bool             `json:"pinned"`
	Author       string           `json:"author"`
	Role         string           `json:"role"`
	Date         string           `json:"date"`
	Title        string           `json:"title"`
	Body         string           `json:"body"`
	Media        []map[string]any `json:"media"`
	Reactions    int64            `json:"reactions"`
	Comments     int64            `json:"comments"`
	Identity     string           `json:"identity"`
	Avatar       string           `json:"avatar"`
	DivisionLogo string           `json:"division_logo"`
	PostedBy     string           `json:"posted_by"`
}

type memberFeedPost struct {
	ID         int64  `json:"id"`
	Author     string `json:"author"`
	Role       string `json:"role"`
	Time       string `json:"time"`
	Title      string `json:"title,omitempty"`
	Text       string `json:"text"`
	Reactions  int64  `json:"reactions"`
	Comments   int64  `json:"comments"`
	Visibility string `json:"visibility"`
}

type administrationLinks struct {
	Facebook  string `json:"facebook,omitempty"`
	Instagram string `json:"instagram,omitempty"`
	GitHub    string `json:"github,omitempty"`
	Email     string `json:"email,omitempty"`
	Contact   string `json:"contact,omitempty"`
}

type administrationPerson struct {
	ID        int64               `json:"id"`
	AccountID int64               `json:"account_id"`
	Name      string              `json:"name"`
	Picture   string              `json:"picture,omitempty"`
	Gender    string              `json:"gender,omitempty"`
	Age       int                 `json:"age,omitempty"`
	Positions []string            `json:"positions"`
	Links     administrationLinks `json:"links"`
	SortOrder int                 `json:"sort_order"`
}

type divisionPublicSummary struct {
	ID          int64  `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	MemberCount int64  `json:"member_count"`
	LogoURL     string `json:"logo_url"`
}

type publicContentResponse struct {
	Announcements  []publicAnnouncement    `json:"announcements"`
	Blogs          []publicPost            `json:"blogs"`
	Administration []administrationPerson  `json:"administration"`
	Divisions      []divisionPublicSummary `json:"divisions"`
}

var philippineTime = time.FixedZone("PHT", 8*60*60)

func formatPublicDate(value time.Time) string {
	return value.In(philippineTime).Format("Jan 2, 2006")
}

func formatPublicDateTime(value time.Time) string {
	return value.In(philippineTime).Format("Jan 2, 2006 · 3:04 PM")
}

func (a *appServer) publicContent(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "AITS services are temporarily unavailable"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()

	announcements, err := a.loadPublicAnnouncements(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "could not load announcements"})
		return
	}
	posts, err := a.loadPublicPosts(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "could not load posts"})
		return
	}
	administration, err := a.loadAdministration(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "could not load administration"})
		return
	}
	divisions, err := a.loadDivisionSummaries(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "could not load divisions"})
		return
	}

	writeJSON(w, http.StatusOK, publicContentResponse{
		Announcements: announcements, Blogs: posts, Administration: administration, Divisions: divisions,
	})
}

func (a *appServer) loadPublicAnnouncements(ctx context.Context) ([]publicAnnouncement, error) {
	rows, err := a.db.Pool().Query(ctx, `
		SELECT id, announcement_type, title, description, status_tone,
		       action_label, action_href, published_at
		FROM announcements
		WHERE is_published = TRUE
		ORDER BY published_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]publicAnnouncement, 0, 12)
	for rows.Next() {
		var item publicAnnouncement
		var actionLabel, actionHref sql.NullString
		var publishedAt time.Time
		if err := rows.Scan(&item.ID, &item.Type, &item.Title, &item.Description, &item.Status, &actionLabel, &actionHref, &publishedAt); err != nil {
			return nil, err
		}
		item.Date = formatPublicDate(publishedAt)
		if actionLabel.Valid && actionHref.Valid && actionLabel.String != "" && actionHref.String != "" {
			item.Action = &announcementAction{Label: actionLabel.String, Href: actionHref.String}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *appServer) loadPublicPosts(ctx context.Context) ([]publicPost, error) {
	rows, err := a.db.Pool().Query(ctx, `
		SELECT p.id, p.is_pinned,
		       CASE WHEN COALESCE(p.identity_kind,'member')='member' THEN a.display_name ELSE COALESCE(NULLIF(p.author_label, ''), a.display_name) END,
		       COALESCE(NULLIF(p.role_label, ''), 'Member'),
		       COALESCE(p.published_at, p.created_at),
		       COALESCE(p.title, ''), p.body, p.media::text,
		       (SELECT COUNT(*) FROM post_reactions pr WHERE pr.post_id = p.id),
		       (SELECT COUNT(*) FROM post_comments pc WHERE pc.post_id = p.id AND pc.status = 'visible'),
               COALESCE(p.identity_kind,'member'),COALESCE(a.avatar_url,''),COALESCE(d.logo_url,''),a.display_name
		FROM posts p
		JOIN accounts a ON a.id = p.author_account_id
        LEFT JOIN divisions d ON d.id=p.division_id
		WHERE p.visibility = 'public' AND p.moderation_status = 'published' AND p.deleted_at IS NULL
		ORDER BY COALESCE(p.published_at, p.created_at) DESC, p.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]publicPost, 0, 12)
	for rows.Next() {
		var item publicPost
		var publishedAt time.Time
		var mediaJSON string
		if err := rows.Scan(&item.ID, &item.Pinned, &item.Author, &item.Role, &publishedAt, &item.Title, &item.Body, &mediaJSON, &item.Reactions, &item.Comments, &item.Identity, &item.Avatar, &item.DivisionLogo, &item.PostedBy); err != nil {
			return nil, err
		}
		item.Date = formatPublicDateTime(publishedAt)
		item.Media = make([]map[string]any, 0)
		_ = json.Unmarshal([]byte(mediaJSON), &item.Media)
		items = append(items, item)
	}
	return items, rows.Err()
}

func calculateAge(birth time.Time, now time.Time) int {
	age := now.Year() - birth.Year()
	birthday := time.Date(now.Year(), birth.Month(), birth.Day(), 0, 0, 0, 0, now.Location())
	if now.Before(birthday) {
		age--
	}
	if age < 0 {
		return 0
	}
	return age
}

func (a *appServer) loadAdministration(ctx context.Context) ([]administrationPerson, error) {
	directory := make([]administrationPerson, 0)
	rows, err := a.db.Pool().Query(ctx, `
        SELECT am.id, am.account_id, ac.display_name, COALESCE(ac.avatar_url,''),
               COALESCE(mp.gender,''), mp.birth_date, COALESCE(ac.email,''), COALESCE(mp.contact_number,''),
               COALESCE(mp.facebook_url,''), COALESCE(mp.instagram_url,''), COALESCE(mp.github_url,''), am.sort_order,
               ARRAY(SELECT amp.position_title FROM administration_member_positions amp WHERE amp.administration_member_id=am.id ORDER BY amp.sort_order,amp.position_title)
        FROM administration_members am
        JOIN accounts ac ON ac.id=am.account_id
        LEFT JOIN member_profiles mp ON mp.account_id=ac.id
        WHERE am.is_published=TRUE
          AND (am.starts_on IS NULL OR am.starts_on<=CURRENT_DATE)
          AND (am.ends_on IS NULL OR am.ends_on>=CURRENT_DATE)
        ORDER BY am.sort_order,am.id`)
	if err != nil {
		return directory, err
	}
	defer rows.Close()
	for rows.Next() {
		var person administrationPerson
		var email, contact, facebook, instagram, github string
		var birthDate *time.Time
		if err := rows.Scan(&person.ID, &person.AccountID, &person.Name, &person.Picture, &person.Gender, &birthDate, &email, &contact, &facebook, &instagram, &github, &person.SortOrder, &person.Positions); err != nil {
			return directory, err
		}
		if len(person.Positions) == 0 {
			person.Positions = []string{"AITS"}
		}
		if birthDate != nil {
			person.Age = calculateAge(*birthDate, time.Now().In(philippineTime))
		}
		person.Links = administrationLinks{Facebook: facebook, Instagram: instagram, GitHub: github, Email: email, Contact: contact}
		directory = append(directory, person)
	}
	return directory, rows.Err()
}

func (a *appServer) loadDivisionSummaries(ctx context.Context) ([]divisionPublicSummary, error) {
	rows, err := a.db.Pool().Query(ctx, `SELECT d.id,d.code,d.name,(SELECT COUNT(*) FROM member_profiles mp JOIN accounts a ON a.id=mp.account_id WHERE mp.primary_division_id=d.id AND a.status='active'),COALESCE(d.logo_url,'') FROM divisions d WHERE d.is_active=TRUE ORDER BY d.sort_order,d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]divisionPublicSummary, 0, 3)
	for rows.Next() {
		var d divisionPublicSummary
		if rows.Scan(&d.ID, &d.Code, &d.Name, &d.MemberCount, &d.LogoURL) != nil {
			continue
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

func (a *appServer) memberFeed(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "AITS services are temporarily unavailable"})
		return
	}

	limit := 10
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 25 {
		limit = 25
	}

	var before int64
	if raw := strings.TrimSpace(r.URL.Query().Get("before")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			before = parsed
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := a.db.Pool().Query(ctx, `
		SELECT p.id,
		       CASE WHEN COALESCE(p.identity_kind,'member')='member' THEN a.display_name ELSE COALESCE(NULLIF(p.author_label, ''), a.display_name) END,
		       COALESCE(NULLIF(p.role_label, ''), 'Member'),
		       COALESCE(p.published_at, p.created_at),
		       COALESCE(p.title, ''), p.body,
		       (SELECT COUNT(*) FROM post_reactions pr WHERE pr.post_id = p.id),
		       (SELECT COUNT(*) FROM post_comments pc WHERE pc.post_id = p.id AND pc.status = 'visible'),
		       p.visibility
		FROM posts p
		JOIN accounts a ON a.id = p.author_account_id
		WHERE p.moderation_status = 'published' AND p.deleted_at IS NULL
		  AND p.visibility IN ('community', 'public')
		  AND ($1::bigint = 0 OR p.id < $1)
		ORDER BY p.id DESC
		LIMIT $2`, before, limit+1)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "could not load member feed"})
		return
	}
	defer rows.Close()

	posts := make([]memberFeedPost, 0, limit+1)
	for rows.Next() {
		var item memberFeedPost
		var publishedAt time.Time
		if err := rows.Scan(&item.ID, &item.Author, &item.Role, &publishedAt, &item.Title, &item.Text, &item.Reactions, &item.Comments, &item.Visibility); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "could not read member feed"})
			return
		}
		item.Time = formatPublicDateTime(publishedAt)
		posts = append(posts, item)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "could not read member feed"})
		return
	}

	hasMore := len(posts) > limit
	if hasMore {
		posts = posts[:limit]
	}
	var nextCursor any
	if len(posts) > 0 && hasMore {
		nextCursor = posts[len(posts)-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"posts":       posts,
		"has_more":    hasMore,
		"next_cursor": nextCursor,
	})
}

type presenceMember struct {
	Name            string `json:"name"`
	Username        string `json:"username"`
	Division        string `json:"division"`
	Role            string `json:"role"`
	ConnectedAt     string `json:"connected_at"`
	ConnectionCount int    `json:"connection_count"`
}

type presenceHub struct {
	mu        sync.Mutex
	clients   map[*websocket.Conn]presenceMember
	publicURL string
	upgrader  websocket.Upgrader
}

func newPresenceHub(publicURL string) *presenceHub {
	hub := &presenceHub{
		clients:   make(map[*websocket.Conn]presenceMember),
		publicURL: publicURL,
	}
	hub.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     hub.originAllowed,
	}
	return hub
}

func (h *presenceHub) originAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "127.0.0.1" || host == "localhost" {
		return true
	}
	public, err := url.Parse(h.publicURL)
	return err == nil && strings.EqualFold(u.Hostname(), public.Hostname())
}

func (h *presenceHub) handle(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if username == "" || name == "" {
		http.Error(w, "member identity required", http.StatusBadRequest)
		return
	}
	if len(username) > 80 || len(name) > 160 {
		http.Error(w, "member identity is too long", http.StatusBadRequest)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	member := presenceMember{
		Name:        name,
		Username:    username,
		Division:    strings.TrimSpace(r.URL.Query().Get("division")),
		Role:        strings.TrimSpace(r.URL.Query().Get("role")),
		ConnectedAt: time.Now().In(philippineTime).Format(time.RFC3339),
	}
	if member.Division == "" {
		member.Division = "AITS"
	}
	if member.Role == "" {
		member.Role = "Member"
	}

	h.mu.Lock()
	h.clients[conn] = member
	h.broadcastLocked()
	h.mu.Unlock()

	conn.SetReadLimit(2048)
	_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
		_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	}

	h.mu.Lock()
	delete(h.clients, conn)
	_ = conn.Close()
	h.broadcastLocked()
	h.mu.Unlock()
}

func (h *presenceHub) broadcastLocked() {
	byUsername := make(map[string]presenceMember)
	for _, member := range h.clients {
		key := strings.ToLower(member.Username)
		if existing, ok := byUsername[key]; ok {
			existing.ConnectionCount++
			byUsername[key] = existing
			continue
		}
		member.ConnectionCount = 1
		byUsername[key] = member
	}
	members := make([]presenceMember, 0, len(byUsername))
	for _, member := range byUsername {
		members = append(members, member)
	}
	sort.Slice(members, func(i, j int) bool {
		return strings.ToLower(members[i].Name) < strings.ToLower(members[j].Name)
	})
	payload := map[string]any{"type": "presence", "members": members}
	for conn := range h.clients {
		_ = conn.SetWriteDeadline(time.Now().Add(4 * time.Second))
		if err := conn.WriteJSON(payload); err != nil {
			_ = conn.Close()
			delete(h.clients, conn)
		}
	}
}

func (h *presenceHub) handleMember(w http.ResponseWriter, r *http.Request, member presenceMember) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	member.ConnectedAt = time.Now().In(philippineTime).Format(time.RFC3339)
	h.mu.Lock()
	h.clients[conn] = member
	h.broadcastLocked()
	h.mu.Unlock()
	conn.SetReadLimit(2048)
	_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
		_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	}
	h.mu.Lock()
	delete(h.clients, conn)
	_ = conn.Close()
	h.broadcastLocked()
	h.mu.Unlock()
}

func (h *presenceHub) snapshot() map[string]presenceMember {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make(map[string]presenceMember)
	for _, member := range h.clients {
		key := strings.ToLower(member.Username)
		if current, ok := result[key]; ok {
			current.ConnectionCount++
			result[key] = current
		} else {
			member.ConnectionCount = 1
			result[key] = member
		}
	}
	return result
}
