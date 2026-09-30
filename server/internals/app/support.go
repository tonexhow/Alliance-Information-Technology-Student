package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type supportAttachment struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}
type supportIdentity struct {
	Access   string `json:"access,omitempty"`
	System   string `json:"system,omitempty"`
	Division string `json:"division,omitempty"`
	Identity string `json:"identity,omitempty"`
}
type supportHub struct {
	mu        sync.Mutex
	visitor   map[int64]map[*websocket.Conn]struct{}
	admins    map[*websocket.Conn]int64
	writes    sync.Map
	publicURL string
	upgrader  websocket.Upgrader
}

func newSupportHub(publicURL string) *supportHub {
	h := &supportHub{visitor: make(map[int64]map[*websocket.Conn]struct{}), admins: make(map[*websocket.Conn]int64), publicURL: publicURL}
	h.upgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 8192, CheckOrigin: h.originAllowed}
	return h
}

func (h *supportHub) writeJSON(conn *websocket.Conn, payload any) error {
	lockValue, _ := h.writes.LoadOrStore(conn, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteJSON(payload)
}

func (h *supportHub) forget(conn *websocket.Conn) {
	h.writes.Delete(conn)
}

func (h *supportHub) originAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, e := url.Parse(origin)
	if e != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "127.0.0.1" {
		return true
	}
	p, e := url.Parse(h.publicURL)
	return e == nil && strings.EqualFold(host, p.Hostname())
}

func (a *appServer) supportSocket(w http.ResponseWriter, r *http.Request) {
	switch strings.ToLower(r.URL.Query().Get("mode")) {
	case "visitor":
		a.supportVisitorSocket(w, r)
	case "admin":
		a.supportAdminSocket(w, r)
	default:
		http.Error(w, "unsupported support mode", 400)
	}
}

func (a *appServer) supportVisitorSocket(w http.ResponseWriter, r *http.Request) {
	session, ok := a.currentOAuthSession(r)
	if !ok || session.AccountID == 0 {
		http.Error(w, "visitor sign-in required", 401)
		return
	}
	conn, err := a.support.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	a.support.mu.Lock()
	if a.support.visitor[session.AccountID] == nil {
		a.support.visitor[session.AccountID] = make(map[*websocket.Conn]struct{})
	}
	a.support.visitor[session.AccountID][conn] = struct{}{}
	a.support.mu.Unlock()
	defer func() {
		a.support.mu.Lock()
		delete(a.support.visitor[session.AccountID], conn)
		if len(a.support.visitor[session.AccountID]) == 0 {
			delete(a.support.visitor, session.AccountID)
		}
		a.support.mu.Unlock()
		a.support.forget(conn)
		_ = conn.Close()
	}()
	threadID, _ := a.ensureSupportThread(r.Context(), session.AccountID, r)
	var blocked bool
	_ = a.db.Pool().QueryRow(r.Context(), `SELECT is_blocked FROM support_threads WHERE id=$1`, threadID).Scan(&blocked)
	if blocked {
		_ = a.support.writeJSON(conn, map[string]any{"type": "blocked", "message": "This support conversation is blocked."})
		return
	}
	a.sendSupportHistory(conn, threadID, session.AccountID, false)
	a.markSupportSeen(r.Context(), threadID, "visitor")
	a.support.broadcastAdminThreads(a)
	conn.SetReadLimit(128 << 10)
	for {
		var msg struct {
			Type        string              `json:"type"`
			Body        string              `json:"body"`
			ReplyTo     int64               `json:"reply_to"`
			Attachments []supportAttachment `json:"attachments"`
			MessageID   int64               `json:"message_id"`
			Reaction    string              `json:"reaction"`
		}
		if conn.ReadJSON(&msg) != nil {
			return
		}
		switch msg.Type {
		case "ping":
			_ = a.support.writeJSON(conn, map[string]any{"type": "pong"})
		case "typing":
			a.support.broadcastAdmins(map[string]any{"type": "typing", "thread_id": threadID, "account_id": session.AccountID, "name": session.User.Name, "typing": true})
		case "typing_stop":
			a.support.broadcastAdmins(map[string]any{"type": "typing", "thread_id": threadID, "account_id": session.AccountID, "name": session.User.Name, "typing": false})
		case "react":
			count, active, summary := a.toggleSupportReaction(r.Context(), msg.MessageID, session.AccountID, firstNonEmpty(msg.Reaction, "👍"))
			payload := map[string]any{"type": "reaction", "thread_id": threadID, "message_id": msg.MessageID, "count": count, "summary": summary, "active": active, "account_id": session.AccountID}
			a.support.broadcastThread(session.AccountID, payload)
			a.support.broadcastAdmins(payload)
		case "seen":
			a.markSupportSeen(r.Context(), threadID, "visitor")
		case "send":
			body := strings.TrimSpace(msg.Body)
			attachments := validateSupportAttachments(msg.Attachments)
			if (body == "" && len(attachments) == 0) || len(body) > 6000 {
				continue
			}
			saved, err := a.saveSupportMessage(r.Context(), threadID, session.AccountID, "visitor", body, msg.ReplyTo, attachments, supportIdentity{})
			if err != nil {
				continue
			}
			a.support.broadcastThread(session.AccountID, map[string]any{"type": "message", "message": saved})
			a.support.broadcastAdmins(map[string]any{"type": "message", "thread_id": threadID, "message": saved})
			a.support.broadcastAdminThreads(a)
		}
	}
}

func (a *appServer) supportAdminSocket(w http.ResponseWriter, r *http.Request) {
	member, ok := a.currentMember(r)
	if !ok || !member.IsAdmin {
		http.Error(w, "administrator login required", 401)
		return
	}
	conn, err := a.support.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	a.support.mu.Lock()
	a.support.admins[conn] = member.AccountID
	a.support.mu.Unlock()
	a.sendSupportThreads(conn)
	defer func() {
		a.support.mu.Lock()
		delete(a.support.admins, conn)
		a.support.mu.Unlock()
		a.support.forget(conn)
		_ = conn.Close()
	}()
	conn.SetReadLimit(128 << 10)
	for {
		var msg struct {
			Type        string              `json:"type"`
			ThreadID    int64               `json:"thread_id"`
			Body        string              `json:"body"`
			ReplyTo     int64               `json:"reply_to"`
			Attachments []supportAttachment `json:"attachments"`
			Identity    supportIdentity     `json:"identity"`
			MessageID   int64               `json:"message_id"`
			Reaction    string              `json:"reaction"`
		}
		if conn.ReadJSON(&msg) != nil {
			return
		}
		switch msg.Type {
		case "ping":
			_ = a.support.writeJSON(conn, map[string]any{"type": "pong"})
		case "select":
			a.sendSupportHistory(conn, msg.ThreadID, member.AccountID, true)
			a.markSupportSeen(r.Context(), msg.ThreadID, "admin")
		case "typing", "typing_stop":
			var visitorID int64
			if a.db.Pool().QueryRow(r.Context(), `SELECT visitor_account_id FROM support_threads WHERE id=$1`, msg.ThreadID).Scan(&visitorID) != nil {
				continue
			}
			typing := msg.Type == "typing"
			a.support.broadcastThread(visitorID, map[string]any{"type": "typing", "thread_id": msg.ThreadID, "account_id": member.AccountID, "name": "AITS Administration", "typing": typing})
			a.support.broadcastAdmins(map[string]any{"type": "typing", "thread_id": msg.ThreadID, "account_id": member.AccountID, "name": member.Name, "typing": typing})
		case "react":
			count, active, summary := a.toggleSupportReaction(r.Context(), msg.MessageID, member.AccountID, firstNonEmpty(msg.Reaction, "👍"))
			payload := map[string]any{"type": "reaction", "thread_id": msg.ThreadID, "message_id": msg.MessageID, "count": count, "summary": summary, "active": active, "account_id": member.AccountID}
			a.support.broadcastAdmins(payload)
			var visitorID int64
			if a.db.Pool().QueryRow(r.Context(), `SELECT visitor_account_id FROM support_threads WHERE id=$1`, msg.ThreadID).Scan(&visitorID) == nil {
				a.support.broadcastThread(visitorID, payload)
			}
		case "seen":
			a.markSupportSeen(r.Context(), msg.ThreadID, "admin")
		case "send":
			body := strings.TrimSpace(msg.Body)
			attachments := validateSupportAttachments(msg.Attachments)
			if msg.ThreadID < 1 || (body == "" && len(attachments) == 0) || len(body) > 6000 {
				continue
			}
			var visitorID int64
			if a.db.Pool().QueryRow(r.Context(), `SELECT visitor_account_id FROM support_threads WHERE id=$1`, msg.ThreadID).Scan(&visitorID) != nil {
				continue
			}
			saved, err := a.saveSupportMessage(r.Context(), msg.ThreadID, member.AccountID, "admin", body, msg.ReplyTo, attachments, msg.Identity)
			if err != nil {
				continue
			}
			a.support.broadcastThread(visitorID, map[string]any{"type": "message", "message": publicSupportMessage(saved)})
			a.support.broadcastAdmins(map[string]any{"type": "message", "thread_id": msg.ThreadID, "message": saved})
			a.support.broadcastAdminThreads(a)
		}
	}
}

func validateSupportAttachments(items []supportAttachment) []supportAttachment {
	out := make([]supportAttachment, 0, len(items))
	for _, it := range items {
		if len(out) >= 8 {
			break
		}
		if !strings.HasPrefix(it.URL, "/api/private-media/support/") {
			continue
		}
		it.Name = strings.TrimSpace(it.Name)
		if len(it.Name) > 180 {
			it.Name = it.Name[:180]
		}
		out = append(out, it)
	}
	return out
}
func (a *appServer) ensureSupportThread(ctx context.Context, visitorID int64, r *http.Request) (int64, error) {
	ip := supportClientIP(r)
	ua := strings.TrimSpace(r.UserAgent())
	sum := sha256.Sum256([]byte(fmtSupportFingerprint(visitorID, ip, ua)))
	fingerprint := hex.EncodeToString(sum[:])
	var id int64
	err := a.db.Pool().QueryRow(ctx, `INSERT INTO support_threads(visitor_account_id,status,last_message_at,visitor_ip,visitor_user_agent,visitor_fingerprint) VALUES($1,'open',NOW(),NULLIF($2,''),NULLIF($3,''),$4) ON CONFLICT(visitor_account_id) DO UPDATE SET status=CASE WHEN support_threads.is_blocked THEN support_threads.status ELSE 'open' END,visitor_ip=EXCLUDED.visitor_ip,visitor_user_agent=EXCLUDED.visitor_user_agent,visitor_fingerprint=EXCLUDED.visitor_fingerprint,updated_at=NOW() RETURNING id`, visitorID, ip, ua, fingerprint).Scan(&id)
	return id, err
}

func supportClientIP(r *http.Request) string {
	for _, key := range []string{"CF-Connecting-IP", "X-Real-IP"} {
		if v := strings.TrimSpace(r.Header.Get(key)); v != "" {
			return v
		}
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if first := strings.TrimSpace(strings.Split(forwarded, ",")[0]); first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
func fmtSupportFingerprint(accountID int64, ip, ua string) string {
	return strconv.FormatInt(accountID, 10) + "|" + ip + "|" + ua
}
func supportPublicIdentity(identity supportIdentity, fallback string) string {
	if v := strings.TrimSpace(identity.System); v != "" {
		return v
	}
	if v := strings.TrimSpace(identity.Access); v != "" {
		return v
	}
	if v := strings.TrimSpace(identity.Division); v != "" {
		return v
	}
	if v := strings.TrimSpace(identity.Identity); v != "" {
		return v
	}
	return fallback
}

func (a *appServer) supportPublicAvatar(ctx context.Context, identity supportIdentity, fallback string) string {
	if strings.TrimSpace(identity.Identity) != "" {
		return fallback
	}
	if division := strings.TrimSpace(identity.Division); division != "" {
		var logo string
		_ = a.db.Pool().QueryRow(ctx, `SELECT COALESCE(logo_url,'') FROM divisions WHERE LOWER(name)=LOWER($1) OR LOWER(code)=LOWER($1) ORDER BY is_active DESC,id LIMIT 1`, division).Scan(&logo)
		return logo
	}
	return ""
}

func publicSupportMessage(message map[string]any) map[string]any {
	out := make(map[string]any, len(message))
	for key, value := range message {
		if key == "real_sender" || key == "real_avatar" {
			continue
		}
		out[key] = value
	}
	return out
}

func (a *appServer) saveSupportMessage(ctx context.Context, threadID, senderID int64, kind, body string, replyTo int64, attachments []supportAttachment, identity supportIdentity) (map[string]any, error) {
	attachmentJSON, _ := json.Marshal(attachments)
	identityJSON, _ := json.Marshal(identity)
	var id int64
	var created time.Time
	var reply any = nil
	if replyTo > 0 {
		reply = replyTo
	}
	err := a.db.Pool().QueryRow(ctx, `INSERT INTO support_messages(thread_id,sender_account_id,sender_kind,body,reply_to_id,attachments,reply_identity) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb) RETURNING id,created_at`, threadID, senderID, kind, body, reply, string(attachmentJSON), string(identityJSON)).Scan(&id, &created)
	if err != nil {
		return nil, err
	}
	_, _ = a.db.Pool().Exec(ctx, `UPDATE support_threads SET last_message_at=NOW(),updated_at=NOW() WHERE id=$1`, threadID)
	var name, avatar string
	_ = a.db.Pool().QueryRow(ctx, `SELECT display_name,COALESCE(avatar_url,'') FROM accounts WHERE id=$1`, senderID).Scan(&name, &avatar)
	publicName := name
	publicAvatar := avatar
	if kind == "admin" {
		publicName = supportPublicIdentity(identity, "AITS")
		publicAvatar = a.supportPublicAvatar(ctx, identity, avatar)
	}
	return map[string]any{"id": id, "sender_account_id": senderID, "sender": publicName, "avatar": publicAvatar, "real_sender": name, "real_avatar": avatar, "sender_kind": kind, "body": body, "reply_to": replyTo, "attachments": attachments, "identity": identity, "reactions": 0, "reaction_summary": map[string]int64{}, "seen": false, "time": formatPublicDateTime(created)}, nil
}
func (a *appServer) markSupportSeen(ctx context.Context, threadID int64, viewerKind string) {
	if threadID < 1 {
		return
	}
	var visitorID int64
	_ = a.db.Pool().QueryRow(ctx, `SELECT visitor_account_id FROM support_threads WHERE id=$1`, threadID).Scan(&visitorID)
	if viewerKind == "visitor" {
		result, err := a.db.Pool().Exec(ctx, `UPDATE support_messages SET seen_by_visitor_at=COALESCE(seen_by_visitor_at,NOW()) WHERE thread_id=$1 AND sender_kind='admin' AND seen_by_visitor_at IS NULL`, threadID)
		if err == nil {
			if n := result.RowsAffected(); n > 0 {
				a.support.broadcastAdmins(map[string]any{"type": "seen", "thread_id": threadID, "viewer_kind": "visitor"})
			}
		}
		return
	}
	result, err := a.db.Pool().Exec(ctx, `UPDATE support_messages SET seen_by_admin_at=COALESCE(seen_by_admin_at,NOW()) WHERE thread_id=$1 AND sender_kind='visitor' AND seen_by_admin_at IS NULL`, threadID)
	if err != nil {
		return
	}
	if n := result.RowsAffected(); n > 0 {
		payload := map[string]any{"type": "seen", "thread_id": threadID, "viewer_kind": "admin"}
		if visitorID > 0 {
			a.support.broadcastThread(visitorID, payload)
		}
		a.support.broadcastAdmins(payload)
	}
}

func (a *appServer) toggleSupportReaction(ctx context.Context, messageID, accountID int64, reaction string) (int64, bool, map[string]int64) {
	if messageID < 1 {
		return 0, false, map[string]int64{}
	}
	reaction = strings.TrimSpace(reaction)
	if reaction == "" || len([]rune(reaction)) > 8 {
		reaction = "👍"
	}
	var exists bool
	_ = a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM support_message_reactions WHERE message_id=$1 AND account_id=$2 AND reaction=$3)`, messageID, accountID, reaction).Scan(&exists)
	if exists {
		// Clicking the active reaction again removes it. A support participant may
		// otherwise have only one reaction on a message, matching member chat.
		_, _ = a.db.Pool().Exec(ctx, `DELETE FROM support_message_reactions WHERE message_id=$1 AND account_id=$2`, messageID, accountID)
	} else {
		_, _ = a.db.Pool().Exec(ctx, `DELETE FROM support_message_reactions WHERE message_id=$1 AND account_id=$2`, messageID, accountID)
		_, _ = a.db.Pool().Exec(ctx, `INSERT INTO support_message_reactions(message_id,account_id,reaction) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, messageID, accountID, reaction)
	}
	summary := map[string]int64{}
	rows, err := a.db.Pool().Query(ctx, `SELECT reaction,COUNT(*) FROM support_message_reactions WHERE message_id=$1 GROUP BY reaction ORDER BY MIN(created_at)`, messageID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var key string
			var count int64
			if rows.Scan(&key, &count) == nil {
				summary[key] = count
			}
		}
	}
	var total int64
	for _, count := range summary {
		total += count
	}
	return total, !exists, summary
}

func (a *appServer) sendSupportHistory(conn *websocket.Conn, threadID, currentAccountID int64, adminView bool) {
	if threadID < 1 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	rows, err := a.db.Pool().Query(ctx, `SELECT sm.id,sm.sender_account_id,a.display_name,COALESCE(a.avatar_url,''),sm.sender_kind,sm.body,sm.reply_to_id,sm.attachments::text,sm.reply_identity::text,sm.created_at,(SELECT COUNT(*) FROM support_message_reactions sr WHERE sr.message_id=sm.id),COALESCE((SELECT jsonb_object_agg(x.reaction,x.cnt)::text FROM (SELECT reaction,COUNT(*) AS cnt FROM support_message_reactions WHERE message_id=sm.id GROUP BY reaction) x),'{}'),sm.seen_by_visitor_at,sm.seen_by_admin_at FROM support_messages sm JOIN accounts a ON a.id=sm.sender_account_id WHERE sm.thread_id=$1 ORDER BY sm.id ASC LIMIT 300`, threadID)
	if err != nil {
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, senderID, count int64
		var name, avatar, kind, body, attachmentsText, identityText, reactionSummaryText string
		var reply *int64
		var t time.Time
		var seenVisitor, seenAdmin sql.NullTime
		if rows.Scan(&id, &senderID, &name, &avatar, &kind, &body, &reply, &attachmentsText, &identityText, &t, &count, &reactionSummaryText, &seenVisitor, &seenAdmin) == nil {
			var attachments []supportAttachment
			var identity supportIdentity
			reactionSummary := map[string]int64{}
			_ = json.Unmarshal([]byte(attachmentsText), &attachments)
			_ = json.Unmarshal([]byte(identityText), &identity)
			_ = json.Unmarshal([]byte(reactionSummaryText), &reactionSummary)
			replyID := int64(0)
			if reply != nil {
				replyID = *reply
			}
			publicName, publicAvatar := name, avatar
			if kind == "admin" {
				publicName = supportPublicIdentity(identity, "AITS")
				publicAvatar = a.supportPublicAvatar(ctx, identity, avatar)
			}
			displayName, displayAvatar := publicName, publicAvatar
			if adminView {
				displayName, displayAvatar = name, avatar
			}
			seen := seenVisitor.Valid
			if kind == "visitor" {
				seen = seenAdmin.Valid
			}
			item := map[string]any{"id": id, "sender_account_id": senderID, "sender": displayName, "avatar": displayAvatar, "public_sender": publicName, "sender_kind": kind, "body": body, "reply_to": replyID, "attachments": attachments, "identity": identity, "reactions": count, "reaction_summary": reactionSummary, "mine": senderID == currentAccountID, "seen": seen, "time": formatPublicDateTime(t)}
			if adminView {
				item["real_sender"] = name
				item["real_avatar"] = avatar
			}
			items = append(items, item)
		}
	}
	_ = a.support.writeJSON(conn, map[string]any{"type": "history", "thread_id": threadID, "messages": items})
}

func (a *appServer) supportThreadsSnapshot(ctx context.Context) []map[string]any {
	rows, err := a.db.Pool().Query(ctx, `SELECT st.id,a.id,a.display_name,COALESCE(a.email,''),COALESCE(a.avatar_url,''),st.status,st.last_message_at,COALESCE((SELECT body FROM support_messages sm WHERE sm.thread_id=st.id ORDER BY sm.id DESC LIMIT 1),''),COALESCE(st.visitor_ip,''),COALESCE(st.visitor_user_agent,''),COALESCE(st.visitor_fingerprint,''),st.is_blocked FROM support_threads st JOIN accounts a ON a.id=st.visitor_account_id ORDER BY st.last_message_at DESC LIMIT 120`)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, accountID int64
		var name, email, avatar, status, last, ip, userAgent, fingerprint string
		var blocked bool
		var t time.Time
		if rows.Scan(&id, &accountID, &name, &email, &avatar, &status, &t, &last, &ip, &userAgent, &fingerprint, &blocked) == nil {
			items = append(items, map[string]any{"id": id, "account_id": accountID, "name": name, "email": email, "avatar": avatar, "status": status, "time": formatPublicDateTime(t), "time_iso": t.In(philippineTime).Format(time.RFC3339), "preview": last, "ip": ip, "user_agent": userAgent, "fingerprint": fingerprint, "blocked": blocked})
		}
	}
	return items
}
func (a *appServer) sendSupportThreads(conn *websocket.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_ = a.support.writeJSON(conn, map[string]any{"type": "threads", "threads": a.supportThreadsSnapshot(ctx)})
}
func (h *supportHub) broadcastAdminThreads(a *appServer) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	threads := a.supportThreadsSnapshot(ctx)
	cancel()
	h.broadcastAdmins(map[string]any{"type": "threads", "threads": threads})
}
func (h *supportHub) broadcastAdmins(payload any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.admins {
		_ = c.SetWriteDeadline(time.Now().Add(4 * time.Second))
		if h.writeJSON(c, payload) != nil {
			_ = c.Close()
			delete(h.admins, c)
		}
	}
}
func (h *supportHub) broadcastThread(visitorID int64, payload any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.visitor[visitorID] {
		_ = c.SetWriteDeadline(time.Now().Add(4 * time.Second))
		if h.writeJSON(c, payload) != nil {
			_ = c.Close()
			delete(h.visitor[visitorID], c)
		}
	}
}

func (h *supportHub) disconnectVisitor(visitorID int64, payload any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.visitor[visitorID] {
		_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_ = h.writeJSON(c, payload)
		_ = c.Close()
		h.forget(c)
		delete(h.visitor[visitorID], c)
	}
	delete(h.visitor, visitorID)
}

func (a *appServer) adminSupportThread(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/support/"), "/")
	parts := strings.Split(raw, "/")
	if len(parts) < 2 {
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	threadID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || threadID < 1 {
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}
	action := parts[1]
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	switch action {
	case "block":
		_, err = a.db.Pool().Exec(r.Context(), `UPDATE support_threads SET is_blocked=TRUE,blocked_at=NOW(),blocked_by=$1,status='closed',updated_at=NOW() WHERE id=$2`, actor.AccountID, threadID)
	case "unblock":
		_, err = a.db.Pool().Exec(r.Context(), `UPDATE support_threads SET is_blocked=FALSE,blocked_at=NULL,blocked_by=NULL,status='open',updated_at=NOW() WHERE id=$1`, threadID)
	case "clear":
		_, err = a.db.Pool().Exec(r.Context(), `DELETE FROM support_messages WHERE thread_id=$1`, threadID)
	default:
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "support action failed"})
		return
	}
	var visitorID int64
	_ = a.db.Pool().QueryRow(r.Context(), `SELECT visitor_account_id FROM support_threads WHERE id=$1`, threadID).Scan(&visitorID)
	switch action {
	case "block":
		if visitorID > 0 {
			a.support.disconnectVisitor(visitorID, map[string]any{"type": "blocked", "thread_id": threadID, "message": "This support conversation was blocked by an administrator."})
		}
	case "clear":
		payload := map[string]any{"type": "clear", "thread_id": threadID}
		a.support.broadcastAdmins(payload)
		if visitorID > 0 {
			a.support.broadcastThread(visitorID, payload)
		}
	}
	_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO audit_logs(actor_account_id,action,entity_type,entity_id) VALUES($1,$2,'support_thread',$3)`, actor.AccountID, "support."+action, strconv.FormatInt(threadID, 10))
	a.support.broadcastAdminThreads(a)
	writeJSON(w, 200, map[string]any{"ok": true})
}
