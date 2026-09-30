package app

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *appServer) bugReport(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "bug reporting is temporarily unavailable"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	var req struct {
		Summary     string `json:"summary"`
		Description string `json:"description"`
		PageURL     string `json:"page_url"`
		Name        string `json:"name"`
		Email       string `json:"email"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 128<<10)).Decode(&req) != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid report"})
		return
	}
	req.Summary = strings.TrimSpace(req.Summary)
	req.Description = strings.TrimSpace(req.Description)
	req.PageURL = strings.TrimSpace(req.PageURL)
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)
	if len(req.Summary) < 3 || len(req.Description) < 8 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "add a short summary and enough detail to reproduce the problem"})
		return
	}
	if len(req.Summary) > 220 {
		req.Summary = req.Summary[:220]
	}
	if len(req.Description) > 12000 {
		req.Description = req.Description[:12000]
	}
	if len(req.PageURL) > 1000 {
		req.PageURL = req.PageURL[:1000]
	}
	var account any
	if s, ok := a.currentOAuthSession(r); ok {
		account = s.AccountID
		req.Name = firstNonEmpty(s.User.Name, req.Name)
		req.Email = firstNonEmpty(s.User.Email, req.Email)
	} else if m, ok := a.currentMember(r); ok {
		account = m.AccountID
		req.Name = firstNonEmpty(m.Name, req.Name)
	}
	var id int64
	err := a.db.Pool().QueryRow(r.Context(), `INSERT INTO bug_reports(reporter_account_id,reporter_name,reporter_email,summary,description,page_url,user_agent) VALUES((SELECT id FROM accounts WHERE id=$1),NULLIF($2,''),NULLIF($3,''),$4,$5,NULLIF($6,''),NULLIF($7,'')) RETURNING id`, account, req.Name, req.Email, req.Summary, req.Description, req.PageURL, strings.TrimSpace(r.UserAgent())).Scan(&id)
	if err != nil {
		log.Printf("bug report insert failed: %v", err)
		writeJSON(w, 500, map[string]any{"ok": false, "error": "bug report could not be saved"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": id})
}

func (a *appServer) adminBugReports(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	_ = actor
	switch r.Method {
	case http.MethodGet:
		rows, err := a.db.Pool().Query(r.Context(), `SELECT id,COALESCE(reporter_name,'Anonymous'),COALESCE(reporter_email,''),summary,description,COALESCE(page_url,''),COALESCE(user_agent,''),status,COALESCE(admin_note,''),created_at,updated_at FROM bug_reports ORDER BY CASE status WHEN 'open' THEN 0 WHEN 'reviewing' THEN 1 WHEN 'resolved' THEN 2 ELSE 3 END,created_at DESC LIMIT 500`)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id int64
			var name, email, summary, description, pageURL, ua, status, note string
			var created, updated time.Time
			if rows.Scan(&id, &name, &email, &summary, &description, &pageURL, &ua, &status, &note, &created, &updated) == nil {
				items = append(items, map[string]any{"id": id, "name": name, "email": email, "summary": summary, "description": description, "page_url": pageURL, "user_agent": ua, "status": status, "admin_note": note, "created": formatPublicDateTime(created), "updated": formatPublicDateTime(updated)})
			}
		}
		writeJSON(w, 200, map[string]any{"items": items})
	case http.MethodPatch:
		var req struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
			Note   string `json:"note"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&req) != nil || req.ID < 1 {
			writeJSON(w, 400, map[string]any{"ok": false})
			return
		}
		req.Status = strings.TrimSpace(req.Status)
		switch req.Status {
		case "open", "reviewing", "resolved", "closed":
		default:
			writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid bug report status"})
			return
		}
		if _, err := a.db.Pool().Exec(r.Context(), `UPDATE bug_reports SET status=$1,admin_note=NULLIF($2,''),updated_at=NOW() WHERE id=$3`, req.Status, strings.TrimSpace(req.Note), req.ID); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	case http.MethodDelete:
		id, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("id")), 10, 64)
		if id < 1 {
			writeJSON(w, 400, map[string]any{"ok": false})
			return
		}
		if _, err := a.db.Pool().Exec(r.Context(), `DELETE FROM bug_reports WHERE id=$1 AND status IN ('resolved','closed')`, id); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	default:
		writeJSON(w, 405, map[string]any{"ok": false})
	}
}
