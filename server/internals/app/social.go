package app

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *appServer) socialProfileCard(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, 503, map[string]any{"ok": false, "error": "service unavailable"})
		return
	}
	accountID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("id")), 10, 64)
	if accountID < 1 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "account id is required"})
		return
	}
	viewerMember, isMember := a.currentMember(r)
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	var name, username, email, avatar, kind, division, access, membership string
	var active bool
	err := a.db.Pool().QueryRow(ctx, `
        SELECT a.display_name,COALESCE(a.username,''),COALESCE(a.email,''),COALESCE(a.avatar_url,''),a.account_kind,
               COALESCE(d.name,''),
               COALESCE((SELECT rr.name FROM account_roles ar JOIN roles rr ON rr.id=ar.role_id WHERE ar.account_id=a.id AND ar.is_active=TRUE AND (ar.expires_at IS NULL OR ar.expires_at>NOW()) ORDER BY rr.hierarchy_level DESC LIMIT 1),CASE WHEN a.account_kind='member' THEN 'Member' ELSE 'Visitor' END),
               COALESCE(mp.membership_level,''),a.status='active'
        FROM accounts a
        LEFT JOIN member_profiles mp ON mp.account_id=a.id
        LEFT JOIN divisions d ON d.id=mp.primary_division_id
        WHERE a.id=$1 AND a.account_kind IN ('member','visitor')`, accountID).Scan(&name, &username, &email, &avatar, &kind, &division, &access, &membership, &active)
	if err != nil || !active {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "profile unavailable"})
		return
	}
	if !isMember {
		email = ""
	}
	canChat := isMember && kind == "member" && viewerMember.AccountID != accountID
	writeJSON(w, 200, map[string]any{"ok": true, "profile": map[string]any{
		"account_id": accountID, "name": name, "username": username, "email": email, "avatar": avatar, "account_kind": kind,
		"division": division, "access": access, "membership": membership, "can_chat": canChat,
	}})
}

func (a *appServer) socialMentionSearch(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, 503, map[string]any{"ok": false})
		return
	}
	if _, ok := a.currentMember(r); !ok {
		if _, vok := a.currentOAuthSession(r); !vok {
			writeJSON(w, 401, map[string]any{"ok": false, "error": "sign in required"})
			return
		}
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 80 {
		q = q[:80]
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	rows, err := a.db.Pool().Query(ctx, `
        SELECT a.id,a.display_name,COALESCE(a.username,''),COALESCE(a.avatar_url,''),a.account_kind,COALESCE(d.name,'')
        FROM accounts a
        LEFT JOIN member_profiles mp ON mp.account_id=a.id
        LEFT JOIN divisions d ON d.id=mp.primary_division_id
        WHERE a.status='active' AND a.account_kind IN ('member','visitor')
          AND ($1='' OR a.display_name ILIKE '%'||$1||'%' OR COALESCE(a.username,'') ILIKE '%'||$1||'%')
        ORDER BY CASE WHEN a.account_kind='member' THEN 0 ELSE 1 END,a.display_name
        LIMIT 12`, q)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "mentions unavailable"})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, username, avatar, kind, division string
		if rows.Scan(&id, &name, &username, &avatar, &kind, &division) == nil {
			items = append(items, map[string]any{"account_id": id, "name": name, "username": username, "avatar": avatar, "account_kind": kind, "division": division})
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "results": items})
}
