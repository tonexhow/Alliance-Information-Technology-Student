package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func (a *appServer) analyticsEvent(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false})
		return
	}
	var req struct {
		EventType  string         `json:"event_type"`
		EntityType string         `json:"entity_type"`
		EntityID   string         `json:"entity_id"`
		Metadata   map[string]any `json:"metadata"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 24<<10)).Decode(&req)
	req.EventType = strings.TrimSpace(req.EventType)
	if req.EventType == "" {
		req.EventType = "page_view"
	}
	if len(req.EventType) > 60 {
		req.EventType = req.EventType[:60]
	}
	kind := "anonymous"
	var account any
	if s, ok := a.currentOAuthSession(r); ok {
		kind = "visitor"
		account = s.AccountID
	}
	if m, ok := a.currentMember(r); ok {
		kind = "member"
		account = m.AccountID
	}
	raw, _ := json.Marshal(req.Metadata)
	_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO analytics_events(account_id,visitor_kind,event_type,entity_type,entity_id,metadata) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6::jsonb)`, account, kind, req.EventType, strings.TrimSpace(req.EntityType), strings.TrimSpace(req.EntityID), string(raw))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *appServer) memberMembershipRequest(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	var req struct {
		Membership string `json:"membership"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req) != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid request"})
		return
	}
	value := strings.TrimSpace(req.Membership)
	allowedMembership := map[string]string{"member": "Member", "core member": "Core Member", "officer": "Officer"}
	canonical, validMembership := allowedMembership[strings.ToLower(value)]
	if !validMembership {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "choose Member, Core Member, or Officer"})
		return
	}
	value = canonical
	if strings.EqualFold(value, m.MembershipLevel) {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "that is already your current membership"})
		return
	}
	var pending int
	_ = a.db.Pool().QueryRow(r.Context(), `SELECT COUNT(*) FROM member_change_requests WHERE account_id=$1 AND request_type='membership' AND status='pending'`, m.AccountID).Scan(&pending)
	if pending > 0 {
		writeJSON(w, 409, map[string]any{"ok": false, "error": "you already have a membership request awaiting review"})
		return
	}
	_, err := a.db.Pool().Exec(r.Context(), `INSERT INTO member_change_requests(account_id,request_type,requested_membership,status) VALUES($1,'membership',$2,'pending')`, m.AccountID, value)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "membership request could not be submitted"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Membership request submitted for review."})
}

func (a *appServer) memberEmailSecurity(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	action := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/member/security/email/"), "/")
	switch action {
	case "request":
		var req struct{ Email, Password string }
		if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid request"})
			return
		}
		req.Email = strings.TrimSpace(req.Email)
		if !validEmailAddress(req.Email) || req.Password == "" {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "enter a valid new email and your current password"})
			return
		}
		var hash string
		if a.db.Pool().QueryRow(r.Context(), `SELECT COALESCE(password_hash,'') FROM accounts WHERE id=$1`, m.AccountID).Scan(&hash) != nil || hash == "" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
			writeJSON(w, 401, map[string]any{"ok": false, "error": "current password is incorrect"})
			return
		}
		var exists bool
		_ = a.db.Pool().QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM accounts WHERE LOWER(email)=LOWER($1) AND id<>$2)`, req.Email, m.AccountID).Scan(&exists)
		if exists {
			writeJSON(w, 409, map[string]any{"ok": false, "error": "that email is already in use"})
			return
		}
		if err := a.createAndSendSecurityCode(r.Context(), m.AccountID, req.Email, "email_change", m.Name, map[string]string{"email": req.Email}, true); err != nil {
			writeSecurityCodeRequestError(w, err, "verification email could not be sent")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "message": "A 15-minute verification code was sent to your new email.", "expires_in_seconds": 900})
	case "confirm":
		var req struct {
			Code string `json:"code"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req) != nil || strings.TrimSpace(req.Code) == "" {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "enter the verification code"})
			return
		}
		var id int64
		var hash, payload string
		err := a.db.Pool().QueryRow(r.Context(), `SELECT id,code_hash,payload::text FROM account_security_codes WHERE account_id=$1 AND purpose='email_change' AND used_at IS NULL AND expires_at>NOW() ORDER BY id DESC LIMIT 1`, m.AccountID).Scan(&id, &hash, &payload)
		if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(strings.TrimSpace(req.Code))) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "verification code is invalid or expired"})
			return
		}
		var p map[string]string
		_ = json.Unmarshal([]byte(payload), &p)
		email := strings.TrimSpace(p["email"])
		if !validEmailAddress(email) {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "pending email is invalid"})
			return
		}
		tx, err := a.db.Pool().Begin(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		defer tx.Rollback(r.Context())
		if _, err = tx.Exec(r.Context(), `UPDATE accounts SET email=$1,updated_at=NOW() WHERE id=$2`, email, m.AccountID); err != nil {
			writeJSON(w, 409, map[string]any{"ok": false, "error": "that email is already in use"})
			return
		}
		_, _ = tx.Exec(r.Context(), `UPDATE account_security_codes SET used_at=NOW(),updated_at=NOW() WHERE id=$1`, id)
		if tx.Commit(r.Context()) != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		m.Email = email
		a.refreshMemberSession(r, m)
		writeJSON(w, 200, map[string]any{"ok": true, "message": "Email updated."})
	default:
		writeJSON(w, 404, map[string]any{"ok": false})
	}
}

func (a *appServer) clearMemberNotifications(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false})
		return
	}
	_, err := a.db.Pool().Exec(r.Context(), `DELETE FROM notifications WHERE account_id=$1`, m.AccountID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *appServer) adminAnnouncements(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		rows, err := a.db.Pool().Query(r.Context(), `SELECT id,announcement_type,title,description,status_tone,COALESCE(action_label,''),COALESCE(action_href,''),is_published,published_at FROM announcements ORDER BY published_at DESC,id DESC`)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id int64
			var typ, title, desc, tone, label, href string
			var published bool
			var t time.Time
			if rows.Scan(&id, &typ, &title, &desc, &tone, &label, &href, &published, &t) == nil {
				out = append(out, map[string]any{"id": id, "type": typ, "title": title, "description": desc, "status": tone, "action_label": label, "action_href": href, "published": published, "time": formatPublicDateTime(t)})
			}
		}
		writeJSON(w, 200, map[string]any{"announcements": out})
	case http.MethodPost:
		var req struct {
			ID                                                        int64 `json:"id"`
			Type, Title, Description, Status, ActionLabel, ActionHref string
			Published                                                 bool `json:"published"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 48<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false})
			return
		}
		req.Title = strings.TrimSpace(req.Title)
		req.Description = strings.TrimSpace(req.Description)
		if req.Title == "" || req.Description == "" {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "title and description are required"})
			return
		}
		if req.Type == "" {
			req.Type = "General"
		}
		if req.Status == "" {
			req.Status = "info"
		}
		becamePublished := false
		if req.ID > 0 {
			var wasPublished bool
			_ = a.db.Pool().QueryRow(r.Context(), `SELECT is_published FROM announcements WHERE id=$1`, req.ID).Scan(&wasPublished)
			_, err := a.db.Pool().Exec(r.Context(), `UPDATE announcements SET announcement_type=$1,title=$2,description=$3,status_tone=$4,action_label=NULLIF($5,''),action_href=NULLIF($6,''),is_published=$7,published_at=CASE WHEN $7 AND NOT is_published THEN NOW() ELSE published_at END,updated_at=NOW() WHERE id=$8`, req.Type, req.Title, req.Description, req.Status, req.ActionLabel, req.ActionHref, req.Published, req.ID)
			if err != nil {
				writeJSON(w, 500, map[string]any{"ok": false, "error": "announcement could not be updated"})
				return
			}
			becamePublished = req.Published && !wasPublished
		} else {
			_, err := a.db.Pool().Exec(r.Context(), `INSERT INTO announcements(announcement_type,title,description,status_tone,action_label,action_href,is_published,published_at) VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,NOW())`, req.Type, req.Title, req.Description, req.Status, req.ActionLabel, req.ActionHref, req.Published)
			if err != nil {
				writeJSON(w, 500, map[string]any{"ok": false, "error": "announcement could not be added"})
				return
			}
			becamePublished = req.Published
		}
		if becamePublished {
			body := req.Description
			if len(body) > 180 {
				body = body[:180] + "…"
			}
			rows, notifyErr := a.db.Pool().Query(r.Context(), `INSERT INTO notifications(account_id,notification_type,title,body,action_url) SELECT mp.account_id,'announcement',$1,$2,'/member' FROM member_profiles mp JOIN accounts ac ON ac.id=mp.account_id WHERE ac.status='active' RETURNING account_id`, "New announcement · "+req.Title, body)
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
		_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO audit_logs(actor_account_id,action,entity_type,details) VALUES($1,'announcement.save','announcement',jsonb_build_object('title',$2))`, actor.AccountID, req.Title)
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		writeJSON(w, 405, map[string]any{"ok": false})
	}
}

func (a *appServer) adminAnnouncementItem(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodDelete {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	id, err := strconv.ParseInt(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/announcements/"), "/"), 10, 64)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}
	var title string
	var payload string
	if a.db.Pool().QueryRow(r.Context(), `SELECT title,to_jsonb(a)::text FROM announcements a WHERE id=$1`, id).Scan(&title, &payload) != nil {
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	tx, err := a.db.Pool().Begin(r.Context())
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer tx.Rollback(r.Context())
	_, _ = tx.Exec(r.Context(), `INSERT INTO archive_items(entity_type,entity_id,label,payload,archived_by) VALUES('announcement',$1,$2,$3::jsonb,$4)`, fmt.Sprint(id), title, payload, actor.AccountID)
	_, err = tx.Exec(r.Context(), `DELETE FROM announcements WHERE id=$1`, id)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *appServer) adminDivisionItem(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/divisions/"), "/")
	parts := strings.Split(raw, "/")
	if len(parts) < 1 {
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}
	if len(parts) == 2 && parts[1] == "assessments" {
		a.adminDivisionAssessments(w, r, id)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var req struct {
			Name, Code, Description, LogoURL string
			Active                           *bool `json:"active"`
			SortOrder                        int   `json:"sort_order"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false})
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.Code = strings.TrimSpace(req.Code)
		if req.Name == "" || req.Code == "" {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "division name and code are required"})
			return
		}
		active := true
		if req.Active != nil {
			active = *req.Active
		}
		_, err := a.db.Pool().Exec(r.Context(), `UPDATE divisions SET name=$1,code=$2,description=NULLIF($3,''),logo_url=NULLIF($4,''),is_active=$5,sort_order=$6,updated_at=NOW() WHERE id=$7`, req.Name, req.Code, strings.TrimSpace(req.Description), strings.TrimSpace(req.LogoURL), active, req.SortOrder, id)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "division could not be updated"})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	case http.MethodDelete:
		var name, payload string
		if a.db.Pool().QueryRow(r.Context(), `SELECT name,to_jsonb(d)::text FROM divisions d WHERE id=$1`, id).Scan(&name, &payload) != nil {
			writeJSON(w, 404, map[string]any{"ok": false})
			return
		}
		var members int64
		_ = a.db.Pool().QueryRow(r.Context(), `SELECT COUNT(*) FROM member_profiles WHERE primary_division_id=$1`, id).Scan(&members)
		if members > 0 {
			writeJSON(w, 409, map[string]any{"ok": false, "error": "move members out of this division before deleting it"})
			return
		}
		tx, err := a.db.Pool().Begin(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		defer tx.Rollback(r.Context())
		_, _ = tx.Exec(r.Context(), `INSERT INTO archive_items(entity_type,entity_id,label,payload,archived_by) VALUES('division',$1,$2,$3::jsonb,$4)`, fmt.Sprint(id), name, payload, actor.AccountID)
		_, err = tx.Exec(r.Context(), `DELETE FROM divisions WHERE id=$1`, id)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "division is still in use"})
			return
		}
		if tx.Commit(r.Context()) != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		writeJSON(w, 405, map[string]any{"ok": false})
	}
}

func (a *appServer) adminDivisionCreate(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	var req struct {
		Name, Code, Description, LogoURL string
		SortOrder                        int `json:"sort_order"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&req) != nil {
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Code = strings.TrimSpace(strings.ToLower(req.Code))
	if req.Name == "" || req.Code == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "division name and code are required"})
		return
	}
	var id int64
	err := a.db.Pool().QueryRow(r.Context(), `INSERT INTO divisions(code,name,description,logo_url,sort_order) VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),$5) RETURNING id`, req.Code, req.Name, strings.TrimSpace(req.Description), strings.TrimSpace(req.LogoURL), req.SortOrder).Scan(&id)
	if err != nil {
		writeJSON(w, 409, map[string]any{"ok": false, "error": "division name or code already exists"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": id})
}

type assessmentContent struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	LibraryID int64  `json:"library_id,omitempty"`
	URL       string `json:"url,omitempty"`
	Label     string `json:"label,omitempty"`
}

func assessmentContentLabel(c assessmentContent) string {
	if strings.EqualFold(c.Type, "text") || c.Type == "" {
		return strings.TrimSpace(c.Text)
	}
	if strings.TrimSpace(c.Label) != "" {
		return strings.TrimSpace(c.Label)
	}
	return strings.TrimSpace(c.URL)
}

func (a *appServer) normalizeAssessmentContent(ctx context.Context, c assessmentContent, fallback string) (assessmentContent, error) {
	c.Type = strings.ToLower(strings.TrimSpace(c.Type))
	if c.Type == "" {
		c.Type = "text"
	}
	if c.Type == "text" {
		c.Text = strings.TrimSpace(firstNonEmpty(c.Text, fallback))
		c.LibraryID = 0
		c.URL = ""
		c.Label = ""
		if c.Text == "" {
			return c, fmt.Errorf("text content is required")
		}
		return c, nil
	}
	if c.Type != "image" && c.Type != "audio" && c.Type != "video" {
		return c, fmt.Errorf("assessment content type must be text, image, audio, or video")
	}
	c.URL = strings.TrimSpace(c.URL)
	c.Label = strings.TrimSpace(c.Label)
	if c.LibraryID > 0 {
		var url, label, mediaType string
		if a.db.Pool().QueryRow(ctx, `SELECT url,label,media_type FROM media_library WHERE id=$1 AND is_active=TRUE`, c.LibraryID).Scan(&url, &label, &mediaType) != nil {
			return c, fmt.Errorf("selected Library item is unavailable")
		}
		if mediaType != c.Type {
			return c, fmt.Errorf("selected Library item is %s, not %s", mediaType, c.Type)
		}
		c.Text = ""
		c.URL = url
		c.Label = label
		return c, nil
	}
	if c.URL == "" {
		return c, fmt.Errorf("choose a Library item or provide a URL/path for %s content", c.Type)
	}
	if strings.HasPrefix(c.URL, "/library/") {
		var id int64
		var label, mediaType string
		err := a.db.Pool().QueryRow(ctx, `SELECT id,label,media_type FROM media_library WHERE url=$1 AND is_active=TRUE ORDER BY id DESC LIMIT 1`, c.URL).Scan(&id, &label, &mediaType)
		if err == nil {
			if mediaType != c.Type {
				return c, fmt.Errorf("Library path is %s, not %s", mediaType, c.Type)
			}
			c.LibraryID = id
			if c.Label == "" {
				c.Label = label
			}
		}
	} else {
		u, err := url.Parse(c.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return c, fmt.Errorf("%s content URL must use http(s) or an existing /library/ path", c.Type)
		}
	}
	if c.Label == "" {
		clean := strings.TrimSuffix(c.URL, "/")
		if i := strings.LastIndex(clean, "/"); i >= 0 && i < len(clean)-1 {
			c.Label = clean[i+1:]
		}
		if c.Label == "" {
			c.Label = c.Type + " content"
		}
	}
	c.Text = ""
	return c, nil
}

func assessmentContentKey(c assessmentContent) string {
	if c.Type == "text" {
		return "text:" + strings.ToLower(strings.TrimSpace(c.Text))
	}
	if c.LibraryID > 0 {
		return c.Type + ":library:" + strconv.FormatInt(c.LibraryID, 10)
	}
	return c.Type + ":url:" + strings.ToLower(strings.TrimSpace(c.URL))
}

func (a *appServer) adminDivisionAssessments(w http.ResponseWriter, r *http.Request, divisionID int64) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		rows, err := a.db.Pool().Query(r.Context(), `SELECT id,question_text,COALESCE(correct_answer,''),dummy_answers::text,is_active,sort_order,assessment_kind,question_content::text,correct_answer_content::text,dummy_answer_contents::text,assessment_metadata::text,year_level FROM assessment_questions WHERE division_id=$1 ORDER BY year_level,sort_order,id`, divisionID)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id int64
			var question, correct, dummies, kind, qraw, craw, draw, mraw string
			var active bool
			var sort, yearLevel int
			if rows.Scan(&id, &question, &correct, &dummies, &active, &sort, &kind, &qraw, &craw, &draw, &mraw, &yearLevel) != nil {
				continue
			}
			var ds []string
			var qc, cc assessmentContent
			var dc []assessmentContent
			var metadata map[string]any
			_ = json.Unmarshal([]byte(dummies), &ds)
			_ = json.Unmarshal([]byte(qraw), &qc)
			_ = json.Unmarshal([]byte(craw), &cc)
			_ = json.Unmarshal([]byte(draw), &dc)
			_ = json.Unmarshal([]byte(mraw), &metadata)
			if qc.Type == "" {
				qc = assessmentContent{Type: "text", Text: question}
			}
			if cc.Type == "" {
				cc = assessmentContent{Type: "text", Text: correct}
			}
			if len(dc) == 0 {
				for _, v := range ds {
					dc = append(dc, assessmentContent{Type: "text", Text: v})
				}
			}
			out = append(out, map[string]any{"id": id, "question": question, "correctAnswer": correct, "dummyAnswers": ds, "active": active, "sort_order": sort, "kind": kind, "questionContent": qc, "correctContent": cc, "dummyContents": dc, "metadata": metadata, "year_level": yearLevel})
		}
		writeJSON(w, 200, map[string]any{"questions": out})
	case http.MethodPost:
		var req struct {
			ID              int64               `json:"id"`
			Question        string              `json:"question"`
			CorrectAnswer   string              `json:"correctAnswer"`
			DummyAnswers    []string            `json:"dummyAnswers"`
			Active          *bool               `json:"active"`
			SortOrder       int                 `json:"sort_order"`
			Kind            string              `json:"kind"`
			QuestionContent assessmentContent   `json:"questionContent"`
			CorrectContent  assessmentContent   `json:"correctContent"`
			DummyContents   []assessmentContent `json:"dummyContents"`
			Metadata        map[string]any      `json:"metadata"`
			YearLevel       int                 `json:"year_level"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid assessment question"})
			return
		}
		qc, err := a.normalizeAssessmentContent(r.Context(), req.QuestionContent, req.Question)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		cc, err := a.normalizeAssessmentContent(r.Context(), req.CorrectContent, req.CorrectAnswer)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		dummyInput := req.DummyContents
		if len(dummyInput) == 0 {
			for _, v := range req.DummyAnswers {
				dummyInput = append(dummyInput, assessmentContent{Type: "text", Text: v})
			}
		}
		dc := []assessmentContent{}
		seen := map[string]bool{assessmentContentKey(cc): true}
		for _, candidate := range dummyInput {
			normalized, nerr := a.normalizeAssessmentContent(r.Context(), candidate, "")
			if nerr != nil {
				continue
			}
			key := assessmentContentKey(normalized)
			if key != "" && !seen[key] {
				seen[key] = true
				dc = append(dc, normalized)
			}
		}
		if len(dc) < 3 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "add at least three unique dummy answers; the correct answer cannot be repeated"})
			return
		}
		active := true
		if req.Active != nil {
			active = *req.Active
		}
		if req.SortOrder < 1 {
			req.SortOrder = 1
		}
		if req.YearLevel < 1 || req.YearLevel > 4 {
			req.YearLevel = 1
		}
		kind := strings.TrimSpace(req.Kind)
		if kind == "" {
			kind = "qa"
		}
		if req.Metadata == nil {
			req.Metadata = map[string]any{}
		}
		question := assessmentContentLabel(qc)
		correct := assessmentContentLabel(cc)
		dummies := make([]string, 0, len(dc))
		for _, v := range dc {
			dummies = append(dummies, assessmentContentLabel(v))
		}
		rawD, _ := json.Marshal(dummies)
		rawQC, _ := json.Marshal(qc)
		rawCC, _ := json.Marshal(cc)
		rawDC, _ := json.Marshal(dc)
		rawMeta, _ := json.Marshal(req.Metadata)
		if req.ID > 0 {
			_, err = a.db.Pool().Exec(r.Context(), `UPDATE assessment_questions SET question_text=$1,correct_answer=$2,dummy_answers=$3::jsonb,is_active=$4,sort_order=$5,assessment_kind=$6,question_content=$7::jsonb,correct_answer_content=$8::jsonb,dummy_answer_contents=$9::jsonb,assessment_metadata=$10::jsonb,year_level=$11,updated_at=NOW() WHERE id=$12 AND division_id=$13`, question, correct, string(rawD), active, req.SortOrder, kind, string(rawQC), string(rawCC), string(rawDC), string(rawMeta), req.YearLevel, req.ID, divisionID)
		} else {
			_, err = a.db.Pool().Exec(r.Context(), `INSERT INTO assessment_questions(division_id,category,question_text,correct_answer,dummy_answers,is_active,sort_order,assessment_kind,question_content,correct_answer_content,dummy_answer_contents,assessment_metadata,year_level) VALUES($1,'division',$2,$3,$4::jsonb,$5,$6,$7,$8::jsonb,$9::jsonb,$10::jsonb,$11::jsonb,$12)`, divisionID, question, correct, string(rawD), active, req.SortOrder, kind, string(rawQC), string(rawCC), string(rawDC), string(rawMeta), req.YearLevel)
		}
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "question could not be saved"})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	case http.MethodDelete:
		qid, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
		if qid < 1 {
			writeJSON(w, 400, map[string]any{"ok": false})
			return
		}
		_, err := a.db.Pool().Exec(r.Context(), `DELETE FROM assessment_questions WHERE id=$1 AND division_id=$2`, qid, divisionID)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		writeJSON(w, 405, map[string]any{"ok": false})
	}
}

func (a *appServer) adminArchive(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		rows, err := a.db.Pool().Query(r.Context(), `SELECT id,entity_type,entity_id,COALESCE(label,''),archived_at,expires_at FROM archive_items WHERE restored_at IS NULL AND permanently_deleted_at IS NULL AND expires_at>NOW() ORDER BY archived_at DESC`)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id int64
			var typ, eid, label string
			var archived, expires time.Time
			if rows.Scan(&id, &typ, &eid, &label, &archived, &expires) == nil {
				out = append(out, map[string]any{"id": id, "type": typ, "entity_id": eid, "label": label, "archived": formatPublicDateTime(archived), "expires": formatPublicDateTime(expires)})
			}
		}
		writeJSON(w, 200, map[string]any{"items": out})
		return
	}
	if r.Method == http.MethodDelete && r.URL.Query().Get("clear") == "1" {
		rows, err := a.db.Pool().Query(r.Context(), `SELECT id,payload::text FROM archive_items WHERE entity_type='library' AND restored_at IS NULL AND permanently_deleted_at IS NULL`)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "could not inspect archived Library files"})
			return
		}
		type archivedLibrary struct {
			id      int64
			payload string
		}
		items := make([]archivedLibrary, 0)
		for rows.Next() {
			var item archivedLibrary
			if rows.Scan(&item.id, &item.payload) == nil {
				items = append(items, item)
			}
		}
		rows.Close()
		for _, item := range items {
			if err := removeManagedLibraryFileFromArchivePayload(item.payload); err != nil {
				writeJSON(w, 500, map[string]any{"ok": false, "error": "could not permanently remove an archived Library file"})
				return
			}
		}
		appRows, appErr := a.db.Pool().Query(r.Context(), `SELECT entity_id FROM archive_items WHERE entity_type='application' AND restored_at IS NULL AND permanently_deleted_at IS NULL`)
		if appErr != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "could not inspect archived applications"})
			return
		}
		for appRows.Next() {
			var entityID string
			if appRows.Scan(&entityID) == nil {
				if applicationID, parseErr := strconv.ParseInt(strings.TrimSpace(entityID), 10, 64); parseErr == nil && applicationID > 0 {
					if _, deleteErr := a.db.Pool().Exec(r.Context(), `DELETE FROM applications WHERE id=$1`, applicationID); deleteErr != nil {
						appRows.Close()
						writeJSON(w, 500, map[string]any{"ok": false, "error": "could not permanently delete an archived application"})
						return
					}
				}
			}
		}
		appRows.Close()
		_, err = a.db.Pool().Exec(r.Context(), `UPDATE archive_items SET permanently_deleted_at=NOW() WHERE restored_at IS NULL AND permanently_deleted_at IS NULL`)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO audit_logs(actor_account_id,action,entity_type) VALUES($1,'archive.clear','archive')`, actor.AccountID)
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	writeJSON(w, 405, map[string]any{"ok": false})
}

func (a *appServer) adminArchiveItem(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/archive/"), "/")
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}
	var typ, eid, payload string
	if a.db.Pool().QueryRow(r.Context(), `SELECT entity_type,entity_id,payload::text FROM archive_items WHERE id=$1 AND restored_at IS NULL AND permanently_deleted_at IS NULL AND expires_at>NOW()`, id).Scan(&typ, &eid, &payload) != nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "archive item is unavailable"})
		return
	}
	switch parts[1] {
	case "restore":
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]any{"ok": false})
			return
		}
		if err := restoreArchivePayload(r.Context(), a, typ, payload); err != nil {
			writeJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_, _ = a.db.Pool().Exec(r.Context(), `UPDATE archive_items SET restored_at=NOW() WHERE id=$1`, id)
		writeJSON(w, 200, map[string]bool{"ok": true})
	case "delete":
		if r.Method != http.MethodDelete {
			writeJSON(w, 405, map[string]any{"ok": false})
			return
		}
		if typ == "library" {
			if err := removeManagedLibraryFileFromArchivePayload(payload); err != nil {
				writeJSON(w, 500, map[string]any{"ok": false, "error": "could not permanently remove the archived Library file"})
				return
			}
		}
		if typ == "application" {
			applicationID, parseErr := strconv.ParseInt(strings.TrimSpace(eid), 10, 64)
			if parseErr != nil || applicationID < 1 {
				applicationID = parseArchiveEntityID(payload)
			}
			if applicationID > 0 {
				if _, err := a.db.Pool().Exec(r.Context(), `DELETE FROM applications WHERE id=$1`, applicationID); err != nil {
					writeJSON(w, 500, map[string]any{"ok": false, "error": "application could not be permanently deleted"})
					return
				}
			}
		}
		_, _ = a.db.Pool().Exec(r.Context(), `UPDATE archive_items SET permanently_deleted_at=NOW() WHERE id=$1`, id)
		_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO audit_logs(actor_account_id,action,entity_type,entity_id) VALUES($1,'archive.delete','archive',$2)`, actor.AccountID, eid)
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		writeJSON(w, 404, map[string]any{"ok": false})
	}
}

func restoreArchivePayload(ctx context.Context, a *appServer, typ, payload string) error {
	switch typ {
	case "announcement":
		_, err := a.db.Pool().Exec(ctx, `INSERT INTO announcements SELECT * FROM jsonb_populate_record(NULL::announcements,$1::jsonb) ON CONFLICT(id) DO NOTHING`, payload)
		return err
	case "division":
		_, err := a.db.Pool().Exec(ctx, `INSERT INTO divisions SELECT * FROM jsonb_populate_record(NULL::divisions,$1::jsonb) ON CONFLICT(id) DO NOTHING`, payload)
		return err
	case "member":
		var p map[string]any
		if json.Unmarshal([]byte(payload), &p) != nil {
			return fmt.Errorf("archive snapshot is invalid")
		}
		id, _ := strconv.ParseInt(fmt.Sprint(p["id"]), 10, 64)
		if id < 1 {
			return fmt.Errorf("member snapshot is invalid")
		}
		_, err := a.db.Pool().Exec(ctx, `UPDATE accounts SET status='active',updated_at=NOW() WHERE id=$1`, id)
		return err
	case "post":
		_, err := a.db.Pool().Exec(ctx, `UPDATE posts SET moderation_status='published',updated_at=NOW() WHERE id=$1`, parseArchiveEntityID(payload))
		return err
	case "application":
		// Applications are soft-archived: the source record remains intact while
		// the active admin list excludes it. Restoring only reactivates visibility.
		var id int64
		if err := json.Unmarshal([]byte(payload), &map[string]any{}); err != nil {
			return fmt.Errorf("application snapshot is invalid")
		}
		id = parseArchiveEntityID(payload)
		if id < 1 {
			return fmt.Errorf("application snapshot is invalid")
		}
		var exists bool
		if err := a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM applications WHERE id=$1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("the original application no longer exists")
		}
		return nil
	case "library":
		var wrapped struct {
			Item  json.RawMessage `json:"item"`
			Usage json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &wrapped); err == nil && len(wrapped.Item) > 0 && string(wrapped.Item) != "null" {
			tx, err := a.db.Pool().Begin(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			if _, err = tx.Exec(ctx, `INSERT INTO media_library SELECT * FROM jsonb_populate_record(NULL::media_library,$1::jsonb) ON CONFLICT(id) DO NOTHING`, string(wrapped.Item)); err != nil {
				return err
			}
			if len(wrapped.Usage) > 0 && string(wrapped.Usage) != "null" && string(wrapped.Usage) != "[]" {
				if _, err = tx.Exec(ctx, `INSERT INTO media_library_usage SELECT * FROM jsonb_populate_recordset(NULL::media_library_usage,$1::jsonb) ON CONFLICT(id) DO NOTHING`, string(wrapped.Usage)); err != nil {
					return err
				}
			}
			return tx.Commit(ctx)
		}
		// Backward compatibility for library items archived before usage snapshots were added.
		_, err := a.db.Pool().Exec(ctx, `INSERT INTO media_library SELECT * FROM jsonb_populate_record(NULL::media_library,$1::jsonb) ON CONFLICT(id) DO NOTHING`, payload)
		return err
	default:
		return fmt.Errorf("this item cannot be restored automatically")
	}
}
func parseArchiveEntityID(payload string) int64 {
	var p map[string]any
	_ = json.Unmarshal([]byte(payload), &p)
	v := fmt.Sprint(p["id"])
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

func (a *appServer) adminLibraryUsage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireAdmin(w, r); !ok {
		return
	}
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/library-usage/"), "/")
	id, _ := strconv.ParseInt(raw, 10, 64)
	if id < 1 {
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}
	rows, err := a.db.Pool().Query(r.Context(), `SELECT u.id,COALESCE(a.display_name,'System'),u.location_label,u.action,COALESCE(u.reason,''),u.created_at FROM media_library_usage u LEFT JOIN accounts a ON a.id=u.account_id WHERE u.media_id=$1 ORDER BY u.created_at DESC LIMIT 500`, id)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var uid int64
		var who, loc, act, reason string
		var t time.Time
		if rows.Scan(&uid, &who, &loc, &act, &reason, &t) == nil {
			out = append(out, map[string]any{"id": uid, "who": who, "location": loc, "action": act, "reason": reason, "time": formatPublicDateTime(t)})
		}
	}
	var mediaURL string
	_ = a.db.Pool().QueryRow(r.Context(), `SELECT url FROM media_library WHERE id=$1`, id).Scan(&mediaURL)
	refs := a.libraryReferences(r.Context(), id, mediaURL)
	writeJSON(w, 200, map[string]any{"usage": out, "references": refs})
}

func adminRoleLevel(ctx context.Context, a *appServer, accountID int64) (int, string) {
	var level int
	var code string
	_ = a.db.Pool().QueryRow(ctx, `SELECT COALESCE(r.hierarchy_level,0),COALESCE(r.code,'member') FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE ar.account_id=$1 AND ar.is_active=TRUE ORDER BY r.hierarchy_level DESC LIMIT 1`, accountID).Scan(&level, &code)
	return level, code
}

func (a *appServer) adminMemberModeration(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/member-moderation/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || r.Method != http.MethodPost {
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	target, _ := strconv.ParseInt(parts[0], 10, 64)
	if target < 1 || target == actor.AccountID {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "you cannot moderate this account"})
		return
	}
	actorLevel, _ := adminRoleLevel(r.Context(), a, actor.AccountID)
	targetLevel, targetCode := adminRoleLevel(r.Context(), a, target)
	if targetCode == "developer" {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "developer accounts are protected"})
		return
	}
	if targetLevel >= actorLevel {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "you cannot modify a member with the same or higher access level"})
		return
	}
	action := parts[1]
	switch action {
	case "ban", "delete":
		var reason string
		var durationMinutes int
		var forever bool
		if action == "ban" {
			var req struct {
				Reason          string `json:"reason"`
				DurationMinutes int    `json:"duration_minutes"`
				Forever         bool   `json:"forever"`
			}
			if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req) != nil {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "restriction details are required"})
				return
			}
			reason = strings.TrimSpace(req.Reason)
			durationMinutes = req.DurationMinutes
			forever = req.Forever
			if reason == "" || (!forever && durationMinutes < 1) {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "provide a reason and either a lift time or permanent restriction"})
				return
			}
		}
		var name, payload string
		if a.db.Pool().QueryRow(r.Context(), `SELECT display_name,to_jsonb(a)::text FROM accounts a WHERE id=$1 AND account_kind='member'`, target).Scan(&name, &payload) != nil {
			writeJSON(w, 404, map[string]any{"ok": false})
			return
		}
		status := "restricted"
		if action == "delete" {
			status = "disabled"
		}
		tx, err := a.db.Pool().Begin(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		defer tx.Rollback(r.Context())
		if action == "delete" {
			_, _ = tx.Exec(r.Context(), `INSERT INTO archive_items(entity_type,entity_id,label,payload,archived_by) VALUES('member',$1,$2,$3::jsonb,$4)`, fmt.Sprint(target), name, payload, actor.AccountID)
		} else {
			_, _ = tx.Exec(r.Context(), `UPDATE member_restrictions SET lifted_at=COALESCE(lifted_at,NOW()),lifted_by=$2,updated_at=NOW() WHERE account_id=$1 AND lifted_at IS NULL`, target, actor.AccountID)
			if forever {
				_, err = tx.Exec(r.Context(), `INSERT INTO member_restrictions(account_id,reason,restricted_by,lifts_at) VALUES($1,$2,$3,NULL)`, target, reason, actor.AccountID)
			} else {
				_, err = tx.Exec(r.Context(), `INSERT INTO member_restrictions(account_id,reason,restricted_by,lifts_at) VALUES($1,$2,$3,NOW()+($4::text||' minutes')::interval)`, target, reason, actor.AccountID, durationMinutes)
			}
			if err != nil {
				writeJSON(w, 500, map[string]any{"ok": false, "error": "restriction could not be saved"})
				return
			}
		}
		_, err = tx.Exec(r.Context(), `UPDATE accounts SET status=$1,updated_at=NOW() WHERE id=$2`, status, target)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		if tx.Commit(r.Context()) != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		a.invalidateMemberSessions(r.Context(), target, "")
		writeJSON(w, 200, map[string]bool{"ok": true})
	case "restore":
		_, _ = a.db.Pool().Exec(r.Context(), `UPDATE member_restrictions SET lifted_at=COALESCE(lifted_at,NOW()),lifted_by=$2,updated_at=NOW() WHERE account_id=$1 AND lifted_at IS NULL`, target, actor.AccountID)
		_, err := a.db.Pool().Exec(r.Context(), `UPDATE accounts SET status='active',updated_at=NOW() WHERE id=$1`, target)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		writeJSON(w, 400, map[string]any{"ok": false})
	}
}

// keep database/sql imported for nullable helpers used in future workspace queries
var _ = sql.NullString{}
