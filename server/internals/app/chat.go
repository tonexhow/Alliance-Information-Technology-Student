package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type chatAttachment struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

type chatSendPayload struct {
	Body        string           `json:"body"`
	ReplyTo     int64            `json:"reply_to"`
	Attachments []chatAttachment `json:"attachments"`
	ClientNonce string           `json:"client_nonce"`
}
type chatClient struct {
	conn           *websocket.Conn
	accountID      int64
	name           string
	conversationID int64
}
type chatCallRoom struct {
	CallID        string
	HostAccountID int64
	Mode          string
	Participants  map[int64]bool
	StartedAt     time.Time
}

type memberChatHub struct {
	mu        sync.Mutex
	writeMu   sync.Mutex
	clients   map[int64]map[*websocket.Conn]chatClient
	inboxes   map[int64]map[*websocket.Conn]struct{}
	calls     map[int64]*chatCallRoom
	upgrader  websocket.Upgrader
	publicURL string
}

func newMemberChatHub(publicURL string) *memberChatHub {
	h := &memberChatHub{clients: map[int64]map[*websocket.Conn]chatClient{}, inboxes: map[int64]map[*websocket.Conn]struct{}{}, calls: map[int64]*chatCallRoom{}, publicURL: publicURL}
	h.upgrader = websocket.Upgrader{ReadBufferSize: 2048, WriteBufferSize: 8192, CheckOrigin: func(r *http.Request) bool {
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
		p, e := url.Parse(publicURL)
		return e == nil && strings.EqualFold(host, p.Hostname())
	}}
	return h
}
func (h *memberChatHub) add(c chatClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[c.conversationID] == nil {
		h.clients[c.conversationID] = map[*websocket.Conn]chatClient{}
	}
	h.clients[c.conversationID][c.conn] = c
}
func (h *memberChatHub) remove(c chatClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if group := h.clients[c.conversationID]; group != nil {
		delete(group, c.conn)
		if len(group) == 0 {
			delete(h.clients, c.conversationID)
		}
	}
}

func (h *memberChatHub) startCall(conversationID, hostAccountID int64, callID, mode string) (*chatCallRoom, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if current := h.calls[conversationID]; current != nil {
		return current, false
	}
	room := &chatCallRoom{CallID: callID, HostAccountID: hostAccountID, Mode: mode, Participants: map[int64]bool{hostAccountID: true}, StartedAt: time.Now()}
	h.calls[conversationID] = room
	return room, true
}

func (h *memberChatHub) callRoom(conversationID int64) *chatCallRoom {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.calls[conversationID]
	if room == nil {
		return nil
	}
	copyRoom := &chatCallRoom{CallID: room.CallID, HostAccountID: room.HostAccountID, Mode: room.Mode, Participants: map[int64]bool{}, StartedAt: room.StartedAt}
	for id, joined := range room.Participants {
		copyRoom.Participants[id] = joined
	}
	return copyRoom
}

func (h *memberChatHub) joinCall(conversationID, accountID int64, callID string) (*chatCallRoom, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.calls[conversationID]
	if room == nil || room.CallID != callID {
		return nil, false
	}
	room.Participants[accountID] = true
	return room, true
}

func (h *memberChatHub) leaveCall(conversationID, accountID int64, callID string) (ended bool, room *chatCallRoom) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room = h.calls[conversationID]
	if room == nil || room.CallID != callID {
		return false, nil
	}
	if room.HostAccountID == accountID {
		delete(h.calls, conversationID)
		return true, room
	}
	delete(room.Participants, accountID)
	return false, room
}

func (h *memberChatHub) endCall(conversationID, accountID int64, callID string, force bool) (*chatCallRoom, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.calls[conversationID]
	if room == nil || room.CallID != callID {
		return nil, false
	}
	if !force && room.HostAccountID != accountID {
		delete(room.Participants, accountID)
		return room, false
	}
	delete(h.calls, conversationID)
	return room, true
}

func (h *memberChatHub) disconnectCall(conversationID, accountID int64) (payload map[string]any, notify bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// A second tab/device connection for the same account keeps the call alive.
	for _, client := range h.clients[conversationID] {
		if client.accountID == accountID {
			return nil, false
		}
	}
	room := h.calls[conversationID]
	if room == nil || !room.Participants[accountID] {
		return nil, false
	}
	if room.HostAccountID == accountID {
		delete(h.calls, conversationID)
		return map[string]any{"type": "call_end", "conversation_id": conversationID, "call_id": room.CallID, "account_id": accountID, "host_disconnected": true}, true
	}
	delete(room.Participants, accountID)
	return map[string]any{"type": "call_leave", "conversation_id": conversationID, "call_id": room.CallID, "account_id": accountID}, false
}
func (h *memberChatHub) addInbox(accountID int64, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.inboxes[accountID] == nil {
		h.inboxes[accountID] = map[*websocket.Conn]struct{}{}
	}
	h.inboxes[accountID][conn] = struct{}{}
}
func (h *memberChatHub) removeInbox(accountID int64, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if group := h.inboxes[accountID]; group != nil {
		delete(group, conn)
		if len(group) == 0 {
			delete(h.inboxes, accountID)
		}
	}
}
func (h *memberChatHub) notifyAccounts(accountIDs []int64, payload any) {
	h.mu.Lock()
	seen := map[*websocket.Conn]struct{}{}
	connections := make([]*websocket.Conn, 0)
	for _, accountID := range accountIDs {
		for conn := range h.inboxes[accountID] {
			if _, ok := seen[conn]; ok {
				continue
			}
			seen[conn] = struct{}{}
			connections = append(connections, conn)
		}
	}
	h.mu.Unlock()
	failed := make([]*websocket.Conn, 0)
	h.writeMu.Lock()
	for _, conn := range connections {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(payload); err != nil {
			_ = conn.Close()
			failed = append(failed, conn)
		}
	}
	h.writeMu.Unlock()
	if len(failed) == 0 {
		return
	}
	h.mu.Lock()
	for accountID, group := range h.inboxes {
		for _, conn := range failed {
			delete(group, conn)
		}
		if len(group) == 0 {
			delete(h.inboxes, accountID)
		}
	}
	h.mu.Unlock()
}

func (a *appServer) notifyMemberNotification(accountID int64) {
	if a == nil || a.chat == nil || accountID < 1 {
		return
	}
	a.chat.notifyAccounts([]int64{accountID}, map[string]any{"type": "notification_update"})
}

func (h *memberChatHub) sendToConversationAccount(conversationID, accountID int64, payload any) bool {
	h.mu.Lock()
	connections := make([]*websocket.Conn, 0)
	for conn, client := range h.clients[conversationID] {
		if client.accountID == accountID {
			connections = append(connections, conn)
		}
	}
	h.mu.Unlock()
	if len(connections) == 0 {
		return false
	}

	failed := make([]*websocket.Conn, 0)
	sent := false
	h.writeMu.Lock()
	for _, conn := range connections {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(payload); err != nil {
			_ = conn.Close()
			failed = append(failed, conn)
			continue
		}
		sent = true
	}
	h.writeMu.Unlock()

	if len(failed) > 0 {
		h.mu.Lock()
		for _, conn := range failed {
			delete(h.clients[conversationID], conn)
		}
		if len(h.clients[conversationID]) == 0 {
			delete(h.clients, conversationID)
		}
		h.mu.Unlock()
	}
	return sent
}

func (h *memberChatHub) broadcast(conversationID int64, payload any) {
	h.mu.Lock()
	connections := make([]*websocket.Conn, 0, len(h.clients[conversationID]))
	for conn := range h.clients[conversationID] {
		connections = append(connections, conn)
	}
	h.mu.Unlock()

	failed := make([]*websocket.Conn, 0)
	h.writeMu.Lock()
	for _, conn := range connections {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(payload); err != nil {
			_ = conn.Close()
			failed = append(failed, conn)
		}
	}
	h.writeMu.Unlock()
	if len(failed) > 0 {
		h.mu.Lock()
		for _, conn := range failed {
			delete(h.clients[conversationID], conn)
		}
		if len(h.clients[conversationID]) == 0 {
			delete(h.clients, conversationID)
		}
		h.mu.Unlock()
	}
}

func (a *appServer) markConversationSeen(ctx context.Context, conversationID, viewerID int64) ([]int64, error) {
	rows, err := a.db.Pool().Query(ctx, `UPDATE messages SET seen_at=NOW() WHERE conversation_id=$1 AND sender_account_id<>$2 AND seen_at IS NULL AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at>NOW()) AND (view_limit IS NULL OR EXISTS(SELECT 1 FROM message_views mv WHERE mv.message_id=messages.id AND mv.account_id=$2)) RETURNING id`, conversationID, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_, _ = a.db.Pool().Exec(ctx, `UPDATE conversation_members SET last_read_at=NOW(),updated_at=NOW() WHERE conversation_id=$1 AND account_id=$2`, conversationID, viewerID)
	return ids, rows.Err()
}

func (a *appServer) broadcastSeen(ctx context.Context, conversationID, viewerID int64) {
	ids, err := a.markConversationSeen(ctx, conversationID, viewerID)
	if err == nil && len(ids) > 0 {
		a.chat.broadcast(conversationID, map[string]any{"type": "seen", "account_id": viewerID, "message_ids": ids})
	}
}

func parseMessageDirectives(raw string) (body string, viewLimit int, ttlSeconds int) {
	body = strings.TrimSpace(raw)
	for body != "" {
		fields := strings.Fields(body)
		if len(fields) == 0 {
			break
		}
		directive := fields[0]
		lower := strings.ToLower(directive)
		consumed := false
		if strings.HasPrefix(lower, "/limitview-") || strings.HasPrefix(lower, "/limit-") {
			prefix := "/limitview-"
			if strings.HasPrefix(lower, "/limit-") {
				prefix = "/limit-"
			}
			if n, err := strconv.Atoi(strings.TrimPrefix(lower, prefix)); err == nil && n > 0 {
				if n > 100 {
					n = 100
				}
				viewLimit = n
				consumed = true
			}
		}
		if strings.HasPrefix(lower, "/limittime-") {
			if n, err := strconv.Atoi(strings.TrimPrefix(lower, "/limittime-")); err == nil && n > 0 {
				if n < 5 {
					n = 5
				}
				if n > 86400 {
					n = 86400
				}
				ttlSeconds = n
				consumed = true
			}
		}
		if !consumed {
			break
		}
		body = strings.TrimSpace(strings.TrimPrefix(body, directive))
	}
	return body, viewLimit, ttlSeconds
}

func (a *appServer) cleanupUnsentMessages(ctx context.Context) {
	// Unsent messages remain as a visible placeholder for 24 hours, then are
	// physically removed together with reactions/history through FK cascades.
	_, _ = a.db.Pool().Exec(ctx, `DELETE FROM messages WHERE unsent_at IS NOT NULL AND unsent_at<=NOW()-INTERVAL '24 hours'`)
}

func (a *appServer) ensureConversation(ctx context.Context, m memberSessionEntry, scope string, targetID int64) (int64, error) {
	switch scope {
	case "organization":
		var id int64
		err := a.db.Pool().QueryRow(ctx, `INSERT INTO conversations(conversation_type,title,created_by,is_active) VALUES('community','AITS Organization',$1,TRUE) ON CONFLICT (conversation_type) WHERE conversation_type='community' DO UPDATE SET updated_at=NOW() RETURNING id`, m.AccountID).Scan(&id)
		if err != nil {
			return 0, err
		}
		_, _ = a.db.Pool().Exec(ctx, `INSERT INTO conversation_members(conversation_id,account_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, m.AccountID)
		return id, nil
	case "division":
		if m.DivisionID < 1 {
			return 0, fmt.Errorf("division membership required")
		}
		var id int64
		err := a.db.Pool().QueryRow(ctx, `INSERT INTO conversations(conversation_type,title,created_by,division_id,is_active) VALUES('group',$1,$2,$3,TRUE) ON CONFLICT (division_id) WHERE conversation_type='group' AND division_id IS NOT NULL DO UPDATE SET updated_at=NOW() RETURNING id`, firstNonEmpty(m.Division, "Division"), m.AccountID, m.DivisionID).Scan(&id)
		if err != nil {
			return 0, err
		}
		_, _ = a.db.Pool().Exec(ctx, `INSERT INTO conversation_members(conversation_id,account_id) SELECT $1,mp.account_id FROM member_profiles mp JOIN accounts ac ON ac.id=mp.account_id WHERE mp.primary_division_id=$2 AND ac.status='active' ON CONFLICT DO NOTHING`, id, m.DivisionID)
		return id, nil
	case "private":
		if targetID < 1 || targetID == m.AccountID {
			return 0, fmt.Errorf("choose another member")
		}
		var valid bool
		_ = a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=$1 AND account_kind='member' AND status='active')`, targetID).Scan(&valid)
		if !valid {
			return 0, fmt.Errorf("member unavailable")
		}
		var id int64
		err := a.db.Pool().QueryRow(ctx, `SELECT c.id FROM conversations c JOIN conversation_members a ON a.conversation_id=c.id AND a.account_id=$1 JOIN conversation_members b ON b.conversation_id=c.id AND b.account_id=$2 WHERE c.conversation_type='private' AND (SELECT COUNT(*) FROM conversation_members x WHERE x.conversation_id=c.id)=2 LIMIT 1`, m.AccountID, targetID).Scan(&id)
		if err == nil {
			return id, nil
		}
		tx, e := a.db.Pool().Begin(ctx)
		if e != nil {
			return 0, e
		}
		defer tx.Rollback(ctx)
		if e = tx.QueryRow(ctx, `INSERT INTO conversations(conversation_type,title,created_by,is_active) VALUES('private','Private conversation',$1,TRUE) RETURNING id`, m.AccountID).Scan(&id); e != nil {
			return 0, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO conversation_members(conversation_id,account_id,member_role) VALUES($1,$2,'owner'),($1,$3,'member')`, id, m.AccountID, targetID)
		if e != nil {
			return 0, e
		}
		return id, tx.Commit(ctx)
	default:
		return 0, fmt.Errorf("unsupported conversation type")
	}
}

func (a *appServer) memberChats(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	a.cleanupUnsentMessages(r.Context())
	switch r.Method {
	case http.MethodGet:
		// Ensure organization/division channels exist for the signed-in member.
		_, _ = a.ensureConversation(r.Context(), m, "organization", 0)
		if m.DivisionID > 0 {
			_, _ = a.ensureConversation(r.Context(), m, "division", 0)
		}
		rows, err := a.db.Pool().Query(r.Context(), `SELECT c.id,c.conversation_type,COALESCE(c.title,''),COALESCE(d.name,''),COALESCE(d.logo_url,''),COALESCE((SELECT a.display_name FROM conversation_members cm2 JOIN accounts a ON a.id=cm2.account_id WHERE cm2.conversation_id=c.id AND cm2.account_id<>$1 ORDER BY a.display_name LIMIT 1),''),COALESCE((SELECT a.avatar_url FROM conversation_members cm2 JOIN accounts a ON a.id=cm2.account_id WHERE cm2.conversation_id=c.id AND cm2.account_id<>$1 ORDER BY a.display_name LIMIT 1),''),COALESCE((SELECT a.id FROM conversation_members cm2 JOIN accounts a ON a.id=cm2.account_id WHERE cm2.conversation_id=c.id AND cm2.account_id<>$1 ORDER BY a.display_name LIMIT 1),0),COALESCE((SELECT msg.body FROM messages msg WHERE msg.conversation_id=c.id AND msg.deleted_at IS NULL AND msg.unsent_at IS NULL AND (msg.expires_at IS NULL OR msg.expires_at>NOW()) AND (msg.view_limit IS NULL OR msg.sender_account_id=$1 OR EXISTS(SELECT 1 FROM message_views mv WHERE mv.message_id=msg.id AND mv.account_id=$1)) ORDER BY msg.id DESC LIMIT 1),''),COALESCE((SELECT msg.created_at FROM messages msg WHERE msg.conversation_id=c.id AND msg.deleted_at IS NULL AND (msg.expires_at IS NULL OR msg.expires_at>NOW()) AND (msg.view_limit IS NULL OR msg.sender_account_id=$1 OR EXISTS(SELECT 1 FROM message_views mv WHERE mv.message_id=msg.id AND mv.account_id=$1)) ORDER BY msg.id DESC LIMIT 1),c.updated_at),COALESCE((SELECT COUNT(*) FROM messages msg WHERE msg.conversation_id=c.id AND msg.deleted_at IS NULL AND (msg.expires_at IS NULL OR msg.expires_at>NOW()) AND (msg.view_limit IS NULL OR EXISTS(SELECT 1 FROM message_views mv WHERE mv.message_id=msg.id AND mv.account_id=$1)) AND msg.sender_account_id<>$1 AND (cm.last_read_at IS NULL OR msg.created_at>cm.last_read_at)),0) FROM conversations c JOIN conversation_members cm ON cm.conversation_id=c.id AND cm.account_id=$1 LEFT JOIN divisions d ON d.id=c.division_id WHERE c.is_active=TRUE ORDER BY 10 DESC`, m.AccountID)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "conversations unavailable"})
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var id, unreadCount, peerAccountID int64
			var typ, title, division, divisionLogo, other, avatar, last string
			var t time.Time
			if rows.Scan(&id, &typ, &title, &division, &divisionLogo, &other, &avatar, &peerAccountID, &last, &t, &unreadCount) == nil {
				label := title
				scope := "organization"
				if typ == "private" {
					label = other
					scope = "private"
				} else if typ == "group" {
					label = firstNonEmpty(division, title)
					if division != "" {
						scope = "division"
						avatar = firstNonEmpty(divisionLogo, "/logo/aits.png")
					} else {
						scope = "group"
						avatar = "/logo/aits.png"
					}
				} else {
					avatar = "/logo/aits.png"
				}
				item := map[string]any{"id": id, "scope": scope, "name": label, "avatar": avatar, "last": last, "time": formatPublicDateTime(t), "time_iso": t.In(philippineTime).Format(time.RFC3339), "unread": unreadCount > 0, "unread_count": unreadCount, "peer_account_id": peerAccountID}
				if room := a.chat.callRoom(id); room != nil {
					item["active_call"] = map[string]any{"call_id": room.CallID, "mode": room.Mode, "host_account_id": room.HostAccountID, "participant_count": len(room.Participants)}
				}
				out = append(out, item)
			}
		}
		writeJSON(w, 200, map[string]any{"conversations": out})
	case http.MethodPost:
		var req struct {
			Scope     string  `json:"scope"`
			TargetID  int64   `json:"target_account_id"`
			Title     string  `json:"title"`
			MemberIDs []int64 `json:"member_ids"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false})
			return
		}
		scope := strings.ToLower(strings.TrimSpace(req.Scope))
		var id int64
		var err error
		if scope == "group" {
			title := strings.TrimSpace(req.Title)
			if title == "" || len(req.MemberIDs) < 1 {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "group name and at least one member are required"})
				return
			}
			tx, e := a.db.Pool().Begin(r.Context())
			if e != nil {
				writeJSON(w, 500, map[string]any{"ok": false})
				return
			}
			defer tx.Rollback(r.Context())
			e = tx.QueryRow(r.Context(), `INSERT INTO conversations(conversation_type,title,created_by,is_active) VALUES('group',$1,$2,TRUE) RETURNING id`, title, m.AccountID).Scan(&id)
			if e == nil {
				_, e = tx.Exec(r.Context(), `INSERT INTO conversation_members(conversation_id,account_id,member_role) VALUES($1,$2,'owner') ON CONFLICT DO NOTHING`, id, m.AccountID)
			}
			if e == nil {
				for _, mid := range req.MemberIDs {
					if mid < 1 || mid == m.AccountID {
						continue
					}
					_, e = tx.Exec(r.Context(), `INSERT INTO conversation_members(conversation_id,account_id) SELECT $1,id FROM accounts WHERE id=$2 AND account_kind='member' AND status='active' ON CONFLICT DO NOTHING`, id, mid)
					if e != nil {
						break
					}
				}
			}
			if e == nil {
				e = tx.Commit(r.Context())
			}
			err = e
		} else {
			id, err = a.ensureConversation(r.Context(), m, scope, req.TargetID)
		}
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		members, _ := a.conversationAccountIDs(r.Context(), id)
		a.chat.notifyAccounts(members, map[string]any{"type": "conversation_created", "conversation_id": id})
		writeJSON(w, 200, map[string]any{"ok": true, "conversation_id": id})
	default:
		writeJSON(w, 405, map[string]any{"ok": false})
	}
}

func (a *appServer) memberCanAccessConversation(ctx context.Context, accountID, conversationID int64) bool {
	var ok bool
	_ = a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=$1 AND account_id=$2)`, conversationID, accountID).Scan(&ok)
	return ok
}

func (a *appServer) claimLimitedMessageView(ctx context.Context, messageID, accountID, limit int64) bool {
	if messageID < 1 || accountID < 1 || limit < 1 {
		return false
	}
	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		return false
	}
	defer tx.Rollback(ctx)
	var lockedID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM messages WHERE id=$1 AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at>NOW()) FOR UPDATE`, messageID).Scan(&lockedID); err != nil {
		return false
	}
	var already bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM message_views WHERE message_id=$1 AND account_id=$2)`, messageID, accountID).Scan(&already); err != nil {
		return false
	}
	if already {
		return tx.Commit(ctx) == nil
	}
	var count int64
	if err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM message_views WHERE message_id=$1`, messageID).Scan(&count); err != nil || count >= limit {
		return false
	}
	if _, err = tx.Exec(ctx, `INSERT INTO message_views(message_id,account_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, messageID, accountID); err != nil {
		return false
	}
	return tx.Commit(ctx) == nil
}

func (a *appServer) chatPollPayload(ctx context.Context, conversationID, pollID, accountID int64) (map[string]any, error) {
	var id, creatorID, myOption int64
	var question string
	var closesAt *time.Time
	var created time.Time
	if err := a.db.Pool().QueryRow(ctx, `SELECT p.id,COALESCE(p.creator_account_id,0),p.question,p.closes_at,p.created_at,COALESCE((SELECT v.option_id FROM chat_poll_votes v WHERE v.poll_id=p.id AND v.account_id=$3),0) FROM chat_polls p WHERE p.id=$1 AND p.conversation_id=$2`, pollID, conversationID, accountID).Scan(&id, &creatorID, &question, &closesAt, &created, &myOption); err != nil {
		return nil, err
	}
	rows, err := a.db.Pool().Query(ctx, `SELECT o.id,o.option_text,COALESCE((SELECT COUNT(*) FROM chat_poll_votes v WHERE v.option_id=o.id),0) FROM chat_poll_options o WHERE o.poll_id=$1 ORDER BY o.sort_order,o.id`, pollID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	options := []map[string]any{}
	var total int64
	for rows.Next() {
		var optionID, votes int64
		var text string
		if rows.Scan(&optionID, &text, &votes) == nil {
			total += votes
			options = append(options, map[string]any{"id": optionID, "text": text, "votes": votes})
		}
	}
	closed := closesAt != nil && !closesAt.After(time.Now())
	out := map[string]any{"id": id, "creator_account_id": creatorID, "question": question, "created_at": formatPublicDateTime(created), "closed": closed, "my_option_id": myOption, "total_votes": total, "options": options}
	if closesAt != nil {
		out["closes_at"] = closesAt.UTC().Format(time.RFC3339Nano)
	}
	return out, nil
}

func (a *appServer) memberChatItem(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false})
		return
	}
	a.cleanupUnsentMessages(r.Context())
	raw := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/member/chats/"), "/")
	parts := strings.Split(raw, "/")
	id, _ := strconv.ParseInt(parts[0], 10, 64)
	if id < 1 || !a.memberCanAccessConversation(r.Context(), m.AccountID, id) {
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		limit := 50
		if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
			if parsed, err := strconv.Atoi(rawLimit); err == nil {
				if parsed < 10 {
					parsed = 10
				}
				if parsed > 100 {
					parsed = 100
				}
				limit = parsed
			}
		}
		before, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("before")), 10, 64)
		rows, err := a.db.Pool().Query(r.Context(), `SELECT msg.id,COALESCE(msg.sender_account_id,0),COALESCE(a.display_name,'Former member'),COALESCE(a.avatar_url,''),COALESCE(msg.body,''),msg.attachments::text,COALESCE(msg.reply_to_id,0),msg.created_at,msg.seen_at,COALESCE(msg.client_nonce,''),COALESCE((SELECT jsonb_object_agg(x.reaction,x.cnt)::text FROM (SELECT reaction,COUNT(*) cnt FROM message_reactions mr WHERE mr.message_id=msg.id GROUP BY reaction) x),'{}'),msg.edited_at,msg.pinned_at,COALESCE(msg.pinned_by,0),COALESCE(msg.view_limit,0),msg.expires_at,COALESCE((SELECT COUNT(*) FROM message_views mv WHERE mv.message_id=msg.id),0),EXISTS(SELECT 1 FROM message_views mv WHERE mv.message_id=msg.id AND mv.account_id=$2),COALESCE((SELECT COUNT(*) FROM message_history mh WHERE mh.message_id=msg.id AND mh.created_at>=NOW()-INTERVAL '30 days'),0),COALESCE((SELECT mr.reaction FROM message_reactions mr WHERE mr.message_id=msg.id AND mr.account_id=$2),''),msg.unsent_at,COALESCE(msg.poll_id,0) FROM messages msg LEFT JOIN accounts a ON a.id=msg.sender_account_id WHERE msg.conversation_id=$1 AND msg.deleted_at IS NULL AND (msg.unsent_at IS NULL OR msg.unsent_at>NOW()-INTERVAL '24 hours') AND ($3::bigint=0 OR msg.id<$3) AND (msg.expires_at IS NULL OR msg.expires_at>NOW() OR msg.unsent_at IS NOT NULL) AND (msg.view_limit IS NULL OR msg.sender_account_id=$2 OR EXISTS(SELECT 1 FROM message_views mv WHERE mv.message_id=msg.id AND mv.account_id=$2) OR (SELECT COUNT(*) FROM message_views mv WHERE mv.message_id=msg.id)<msg.view_limit) ORDER BY msg.id DESC LIMIT $4`, id, m.AccountID, before, limit+1)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "messages could not be loaded"})
			return
		}
		defer rows.Close()
		rev := []map[string]any{}
		hasMore := false
		for rows.Next() {
			var mid, sender, reply, pinnedBy, viewLimit, viewCount, historyCount, pollID int64
			var name, avatar, body, rawAttach, clientNonce, reactionRaw, myReaction string
			var created time.Time
			var seen, edited, pinnedAt, expiresAt, unsentAt *time.Time
			var viewedByMe bool
			if rows.Scan(&mid, &sender, &name, &avatar, &body, &rawAttach, &reply, &created, &seen, &clientNonce, &reactionRaw, &edited, &pinnedAt, &pinnedBy, &viewLimit, &expiresAt, &viewCount, &viewedByMe, &historyCount, &myReaction, &unsentAt, &pollID) != nil {
				continue
			}
			if len(rev) >= limit {
				hasMore = true
				continue
			}
			if viewLimit > 0 && sender != m.AccountID && !viewedByMe {
				if !a.claimLimitedMessageView(r.Context(), mid, m.AccountID, viewLimit) {
					continue
				}
				viewCount++
				viewedByMe = true
			}
			var at []chatAttachment
			reactionSummary := map[string]int64{}
			_ = json.Unmarshal([]byte(rawAttach), &at)
			_ = json.Unmarshal([]byte(reactionRaw), &reactionSummary)
			total := int64(0)
			for _, count := range reactionSummary {
				total += count
			}
			item := map[string]any{
				"id": mid, "sender_account_id": sender, "sender": name, "avatar": avatar, "body": body,
				"attachments": at, "reply_to": reply, "time": formatPublicDateTime(created), "mine": sender == m.AccountID,
				"seen": seen != nil, "reactions": total, "reaction_summary": reactionSummary, "client_nonce": clientNonce,
				"edited": edited != nil, "history_count": historyCount, "unsent": unsentAt != nil, "pinned": pinnedAt != nil, "pinned_by": pinnedBy, "view_limit": viewLimit, "view_count": viewCount, "my_reaction": myReaction, "poll_id": pollID,
			}
			if edited != nil {
				item["edited_time"] = formatPublicDateTime(*edited)
			}
			if expiresAt != nil {
				item["expires_at"] = expiresAt.UTC().Format(time.RFC3339Nano)
			}
			rev = append(rev, item)
		}
		out := make([]map[string]any, len(rev))
		for i := range rev {
			out[len(rev)-1-i] = rev[i]
		}
		participants := []map[string]any{}
		if memberRows, e := a.db.Pool().Query(r.Context(), `SELECT a.id,a.display_name,COALESCE(a.username,''),COALESCE(a.avatar_url,'') FROM conversation_members cm JOIN accounts a ON a.id=cm.account_id WHERE cm.conversation_id=$1 AND a.status='active' ORDER BY a.display_name`, id); e == nil {
			for memberRows.Next() {
				var pid int64
				var pname, pusername, pavatar string
				if memberRows.Scan(&pid, &pname, &pusername, &pavatar) == nil {
					participants = append(participants, map[string]any{"account_id": pid, "name": pname, "username": pusername, "avatar": pavatar})
				}
			}
			memberRows.Close()
		}
		var nextBefore any
		if hasMore && len(out) > 0 {
			nextBefore = out[0]["id"]
		}
		pins := []map[string]any{}
		if pinRows, e := a.db.Pool().Query(r.Context(), `SELECT msg.id,COALESCE(msg.sender_account_id,0),COALESCE(sender.display_name,'Former member'),COALESCE(msg.body,''),msg.created_at,COALESCE(msg.pinned_by,0),COALESCE(pinner.display_name,'') FROM messages msg LEFT JOIN accounts sender ON sender.id=msg.sender_account_id LEFT JOIN accounts pinner ON pinner.id=msg.pinned_by WHERE msg.conversation_id=$1 AND msg.pinned_at IS NOT NULL AND msg.deleted_at IS NULL AND msg.unsent_at IS NULL AND (msg.expires_at IS NULL OR msg.expires_at>NOW()) AND (msg.view_limit IS NULL OR msg.sender_account_id=$2 OR EXISTS(SELECT 1 FROM message_views mv WHERE mv.message_id=msg.id AND mv.account_id=$2)) ORDER BY msg.pinned_at DESC LIMIT 100`, id, m.AccountID); e == nil {
			for pinRows.Next() {
				var pid, senderID, pinnedBy int64
				var senderName, body, pinnerName string
				var created time.Time
				if pinRows.Scan(&pid, &senderID, &senderName, &body, &created, &pinnedBy, &pinnerName) == nil {
					pins = append(pins, map[string]any{"id": pid, "sender_account_id": senderID, "sender": senderName, "body": body, "time": formatPublicDateTime(created), "pinned": true, "pinned_by": pinnedBy, "pinned_by_name": pinnerName})
				}
			}
			pinRows.Close()
		}
		var activeCall any
		if room := a.chat.callRoom(id); room != nil {
			activeCall = map[string]any{"call_id": room.CallID, "mode": room.Mode, "host_account_id": room.HostAccountID, "participant_count": len(room.Participants)}
		}
		a.broadcastSeen(r.Context(), id, m.AccountID)
		writeJSON(w, 200, map[string]any{"messages": out, "participants": participants, "pins": pins, "has_more": hasMore, "next_before": nextBefore, "active_call": activeCall})
		return
	}
	if len(parts) == 2 && parts[1] == "search" && r.Method == http.MethodGet {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeJSON(w, 200, map[string]any{"results": []map[string]any{}})
			return
		}
		if len([]rune(query)) > 120 {
			query = string([]rune(query)[:120])
		}
		limit := 60
		if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
			if parsed, err := strconv.Atoi(rawLimit); err == nil {
				if parsed < 10 {
					parsed = 10
				}
				if parsed > 100 {
					parsed = 100
				}
				limit = parsed
			}
		}
		rows, err := a.db.Pool().Query(r.Context(), `SELECT msg.id,COALESCE(msg.sender_account_id,0),COALESCE(ac.display_name,'Former member'),COALESCE(msg.body,''),msg.created_at,COALESCE(msg.attachments::text ILIKE '%' || $3 || '%',FALSE) FROM messages msg LEFT JOIN accounts ac ON ac.id=msg.sender_account_id WHERE msg.conversation_id=$1 AND msg.deleted_at IS NULL AND msg.unsent_at IS NULL AND (msg.expires_at IS NULL OR msg.expires_at>NOW()) AND (msg.view_limit IS NULL OR msg.sender_account_id=$2 OR EXISTS(SELECT 1 FROM message_views mv WHERE mv.message_id=msg.id AND mv.account_id=$2)) AND (COALESCE(msg.body,'') ILIKE '%' || $3 || '%' OR COALESCE(msg.attachments::text,'') ILIKE '%' || $3 || '%') ORDER BY msg.id DESC LIMIT $4`, id, m.AccountID, query, limit)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "chat search is temporarily unavailable"})
			return
		}
		defer rows.Close()
		results := []map[string]any{}
		for rows.Next() {
			var messageID, senderID int64
			var sender, body string
			var created time.Time
			var attachmentMatch bool
			if rows.Scan(&messageID, &senderID, &sender, &body, &created, &attachmentMatch) != nil {
				continue
			}
			results = append(results, map[string]any{"id": messageID, "sender_account_id": senderID, "sender": sender, "body": body, "time": formatPublicDateTime(created), "mine": senderID == m.AccountID, "attachment_match": attachmentMatch})
		}
		writeJSON(w, 200, map[string]any{"results": results, "query": query})
		return
	}
	if len(parts) == 2 && parts[1] == "polls" && r.Method == http.MethodPost {
		var req struct {
			Question        string   `json:"question"`
			Options         []string `json:"options"`
			DurationSeconds int      `json:"duration_seconds"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid poll"})
			return
		}
		req.Question = strings.TrimSpace(req.Question)
		if req.Question == "" || len([]rune(req.Question)) > 500 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "poll question is required and must be 500 characters or fewer"})
			return
		}
		clean := make([]string, 0, 8)
		seenOptions := map[string]bool{}
		for _, option := range req.Options {
			option = strings.TrimSpace(option)
			if option == "" || len([]rune(option)) > 240 {
				continue
			}
			key := strings.ToLower(option)
			if seenOptions[key] {
				continue
			}
			seenOptions[key] = true
			clean = append(clean, option)
			if len(clean) == 8 {
				break
			}
		}
		if len(clean) < 2 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "add at least two poll choices"})
			return
		}
		if req.DurationSeconds != 0 && req.DurationSeconds < 60 {
			req.DurationSeconds = 60
		}
		if req.DurationSeconds > 604800 {
			req.DurationSeconds = 604800
		}
		var closes any
		if req.DurationSeconds > 0 {
			closes = time.Now().Add(time.Duration(req.DurationSeconds) * time.Second)
		}
		tx, err := a.db.Pool().Begin(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "poll could not be created"})
			return
		}
		defer tx.Rollback(r.Context())
		var pollID int64
		if err = tx.QueryRow(r.Context(), `INSERT INTO chat_polls(conversation_id,creator_account_id,question,closes_at) VALUES($1,$2,$3,$4) RETURNING id`, id, m.AccountID, req.Question, closes).Scan(&pollID); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "poll could not be created"})
			return
		}
		for index, option := range clean {
			if _, err = tx.Exec(r.Context(), `INSERT INTO chat_poll_options(poll_id,option_text,sort_order) VALUES($1,$2,$3)`, pollID, option, index); err != nil {
				writeJSON(w, 500, map[string]any{"ok": false, "error": "poll choices could not be saved"})
				return
			}
		}
		var messageID int64
		var created time.Time
		body := "📊 " + req.Question
		if err = tx.QueryRow(r.Context(), `INSERT INTO messages(conversation_id,sender_account_id,body,poll_id,pinned_at,pinned_by,attachments) VALUES($1,$2,$3,$4,NOW(),$2,'[]'::jsonb) RETURNING id,created_at`, id, m.AccountID, body, pollID).Scan(&messageID, &created); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "poll message could not be created"})
			return
		}
		_, _ = tx.Exec(r.Context(), `UPDATE conversations SET updated_at=NOW() WHERE id=$1`, id)
		if err = tx.Commit(r.Context()); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "poll could not be committed"})
			return
		}
		message := map[string]any{"id": messageID, "sender_account_id": m.AccountID, "sender": m.Name, "avatar": m.Avatar, "body": body, "attachments": []chatAttachment{}, "reply_to": int64(0), "time": formatPublicDateTime(created), "seen": false, "reactions": 0, "reaction_summary": map[string]int64{}, "edited": false, "pinned": true, "pinned_by": m.AccountID, "poll_id": pollID}
		a.chat.broadcast(id, map[string]any{"type": "message", "message": message})
		if memberIDs, e := a.conversationAccountIDs(r.Context(), id); e == nil {
			a.chat.notifyAccounts(memberIDs, map[string]any{"type": "conversation_update", "conversation_id": id, "message_id": messageID, "sender_account_id": m.AccountID})
		}
		poll, _ := a.chatPollPayload(r.Context(), id, pollID, m.AccountID)
		writeJSON(w, 201, map[string]any{"ok": true, "message": message, "poll": poll})
		return
	}
	if len(parts) == 3 && parts[1] == "polls" && r.Method == http.MethodGet {
		pollID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || pollID < 1 {
			writeJSON(w, 404, map[string]any{"ok": false, "error": "poll not found"})
			return
		}
		poll, err := a.chatPollPayload(r.Context(), id, pollID, m.AccountID)
		if err != nil {
			writeJSON(w, 404, map[string]any{"ok": false, "error": "poll not found"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "poll": poll})
		return
	}
	if len(parts) == 4 && parts[1] == "polls" && parts[3] == "vote" && r.Method == http.MethodPost {
		pollID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || pollID < 1 {
			writeJSON(w, 404, map[string]any{"ok": false, "error": "poll not found"})
			return
		}
		var req struct {
			OptionID int64 `json:"option_id"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req) != nil || req.OptionID < 1 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "choose a poll option"})
			return
		}
		var valid bool
		if err := a.db.Pool().QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM chat_polls p JOIN chat_poll_options o ON o.poll_id=p.id WHERE p.id=$1 AND p.conversation_id=$2 AND o.id=$3 AND (p.closes_at IS NULL OR p.closes_at>NOW()))`, pollID, id, req.OptionID).Scan(&valid); err != nil || !valid {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "this poll is closed or the choice is unavailable"})
			return
		}
		_, err = a.db.Pool().Exec(r.Context(), `INSERT INTO chat_poll_votes(poll_id,option_id,account_id) VALUES($1,$2,$3) ON CONFLICT (poll_id,account_id) DO UPDATE SET option_id=EXCLUDED.option_id,updated_at=NOW()`, pollID, req.OptionID, m.AccountID)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "vote could not be saved"})
			return
		}
		poll, _ := a.chatPollPayload(r.Context(), id, pollID, m.AccountID)
		a.chat.broadcast(id, map[string]any{"type": "poll_update", "poll_id": pollID})
		writeJSON(w, 200, map[string]any{"ok": true, "poll": poll})
		return
	}
	if len(parts) == 2 && parts[1] == "pins" && r.Method == http.MethodGet {
		rows, err := a.db.Pool().Query(r.Context(), `SELECT msg.id,COALESCE(msg.body,''),COALESCE(ac.display_name,'Former member'),COALESCE(msg.pinned_by,0),msg.pinned_at FROM messages msg LEFT JOIN accounts ac ON ac.id=msg.sender_account_id WHERE msg.conversation_id=$1 AND msg.pinned_at IS NOT NULL AND msg.deleted_at IS NULL AND msg.unsent_at IS NULL AND (msg.expires_at IS NULL OR msg.expires_at>NOW()) AND (msg.view_limit IS NULL OR msg.sender_account_id=$2 OR EXISTS(SELECT 1 FROM message_views mv WHERE mv.message_id=msg.id AND mv.account_id=$2)) ORDER BY msg.pinned_at DESC LIMIT 100`, id, m.AccountID)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "pins unavailable"})
			return
		}
		defer rows.Close()
		pins := []map[string]any{}
		for rows.Next() {
			var mid, pinnedBy int64
			var body, sender string
			var at time.Time
			if rows.Scan(&mid, &body, &sender, &pinnedBy, &at) == nil {
				pins = append(pins, map[string]any{"id": mid, "body": body, "sender": sender, "pinned_by": pinnedBy, "pinned_at": formatPublicDateTime(at)})
			}
		}
		writeJSON(w, 200, map[string]any{"ok": true, "pins": pins})
		return
	}
	if len(parts) == 2 && parts[1] == "messages" && r.Method == http.MethodPost {
		var req chatSendPayload
		if json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&req) != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid message"})
			return
		}
		message, err := a.saveChatMessage(r.Context(), id, m, req)
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if limit, _ := message["view_limit"].(int); limit > 0 {
			a.chat.broadcast(id, map[string]any{"type": "message_available", "conversation_id": id, "message_id": message["id"]})
		} else {
			a.chat.broadcast(id, map[string]any{"type": "message", "message": message})
		}
		if memberIDs, e := a.conversationAccountIDs(r.Context(), id); e == nil {
			a.chat.notifyAccounts(memberIDs, map[string]any{"type": "conversation_update", "conversation_id": id, "message_id": message["id"], "sender_account_id": m.AccountID})
		}
		writeJSON(w, 201, map[string]any{"ok": true, "message": message})
		return
	}
	if len(parts) >= 3 && parts[1] == "messages" {
		messageID, parseErr := strconv.ParseInt(parts[2], 10, 64)
		if parseErr != nil || messageID < 1 {
			writeJSON(w, 404, map[string]any{"ok": false, "error": "message not found"})
			return
		}
		if len(parts) == 4 && parts[3] == "reactions" && r.Method == http.MethodGet {
			rows, err := a.db.Pool().Query(r.Context(), `SELECT mr.reaction,ac.id,ac.display_name,COALESCE(ac.avatar_url,'') FROM message_reactions mr JOIN accounts ac ON ac.id=mr.account_id JOIN messages msg ON msg.id=mr.message_id WHERE mr.message_id=$1 AND msg.conversation_id=$2 ORDER BY mr.reaction,ac.display_name`, messageID, id)
			if err != nil {
				writeJSON(w, 500, map[string]any{"ok": false})
				return
			}
			defer rows.Close()
			people := []map[string]any{}
			for rows.Next() {
				var reaction, name, avatar string
				var accountID int64
				if rows.Scan(&reaction, &accountID, &name, &avatar) == nil {
					people = append(people, map[string]any{"reaction": reaction, "account_id": accountID, "name": name, "avatar": avatar})
				}
			}
			writeJSON(w, 200, map[string]any{"ok": true, "people": people})
			return
		}
		if len(parts) == 4 && parts[3] == "history" && r.Method == http.MethodGet {
			rows, err := a.db.Pool().Query(r.Context(), `SELECT mh.id,mh.body,mh.created_at,ac.display_name FROM message_history mh JOIN accounts ac ON ac.id=mh.editor_account_id JOIN messages msg ON msg.id=mh.message_id WHERE mh.message_id=$1 AND msg.conversation_id=$2 AND mh.created_at>=NOW()-INTERVAL '30 days' ORDER BY mh.id DESC LIMIT 100`, messageID, id)
			if err != nil {
				writeJSON(w, 500, map[string]any{"ok": false, "error": "history unavailable"})
				return
			}
			defer rows.Close()
			history := []map[string]any{}
			for rows.Next() {
				var hid int64
				var body, name string
				var at time.Time
				if rows.Scan(&hid, &body, &at, &name) == nil {
					history = append(history, map[string]any{"id": hid, "body": body, "time": formatPublicDateTime(at), "editor": name})
				}
			}
			writeJSON(w, 200, map[string]any{"ok": true, "history": history})
			return
		}
		if len(parts) == 3 && r.Method == http.MethodPatch {
			var req struct {
				Body string `json:"body"`
			}
			if json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req) != nil {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid message"})
				return
			}
			body := strings.TrimSpace(req.Body)
			if body == "" {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "message cannot be empty"})
				return
			}
			if len(body) > 8000 {
				body = body[:8000]
			}
			var senderID int64
			var expiresAt *time.Time
			var viewLimit int
			tx, txErr := a.db.Pool().Begin(r.Context())
			if txErr != nil {
				writeJSON(w, 500, map[string]any{"ok": false, "error": "message could not be edited"})
				return
			}
			defer tx.Rollback(r.Context())
			var oldBody string
			err := tx.QueryRow(r.Context(), `SELECT COALESCE(body,'') FROM messages WHERE id=$1 AND conversation_id=$2 AND sender_account_id=$3 AND deleted_at IS NULL AND unsent_at IS NULL AND (expires_at IS NULL OR expires_at>NOW()) FOR UPDATE`, messageID, id, m.AccountID).Scan(&oldBody)
			if err == nil {
				_, err = tx.Exec(r.Context(), `INSERT INTO message_history(message_id,editor_account_id,body) VALUES($1,$2,$3)`, messageID, m.AccountID, oldBody)
			}
			if err == nil {
				err = tx.QueryRow(r.Context(), `UPDATE messages SET body=$1,edited_at=NOW(),updated_at=NOW() WHERE id=$2 AND conversation_id=$3 AND sender_account_id=$4 AND deleted_at IS NULL AND unsent_at IS NULL AND (expires_at IS NULL OR expires_at>NOW()) RETURNING sender_account_id,expires_at,COALESCE(view_limit,0)`, body, messageID, id, m.AccountID).Scan(&senderID, &expiresAt, &viewLimit)
			}
			if err == nil {
				err = tx.Commit(r.Context())
			}
			if err != nil {
				writeJSON(w, 403, map[string]any{"ok": false, "error": "only your active messages can be edited"})
				return
			}
			payload := map[string]any{"type": "message_edit", "conversation_id": id, "message_id": messageID, "body": body, "edited": true, "edited_time": formatPublicDateTime(time.Now())}
			a.sendMessageScopedPayload(r.Context(), id, messageID, viewLimit, payload)
			writeJSON(w, 200, map[string]any{"ok": true, "message": payload})
			return
		}
		if len(parts) == 3 && r.Method == http.MethodDelete {
			tag, err := a.db.Pool().Exec(r.Context(), `UPDATE messages SET body='Message unsent',attachments='[]'::jsonb,reply_to_id=NULL,pinned_at=NULL,pinned_by=NULL,poll_id=NULL,unsent_at=NOW(),expires_at=NOW()+INTERVAL '24 hours',edited_at=NULL,updated_at=NOW() WHERE id=$1 AND conversation_id=$2 AND sender_account_id=$3 AND deleted_at IS NULL AND unsent_at IS NULL`, messageID, id, m.AccountID)
			if err != nil || tag.RowsAffected() == 0 {
				writeJSON(w, 403, map[string]any{"ok": false, "error": "only your messages can be unsent"})
				return
			}
			a.chat.broadcast(id, map[string]any{"type": "message_unsent", "conversation_id": id, "message_id": messageID, "body": "Message unsent", "expires_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)})
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
		if len(parts) == 4 && parts[3] == "pin" && r.Method == http.MethodPost {
			var req struct {
				Pinned bool `json:"pinned"`
			}
			_ = json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req)
			var viewLimit int
			err := a.db.Pool().QueryRow(r.Context(), `SELECT COALESCE(msg.view_limit,0) FROM messages msg WHERE msg.id=$1 AND msg.conversation_id=$2 AND msg.deleted_at IS NULL AND (msg.expires_at IS NULL OR msg.expires_at>NOW()) AND (msg.view_limit IS NULL OR msg.sender_account_id=$3 OR EXISTS(SELECT 1 FROM message_views mv WHERE mv.message_id=msg.id AND mv.account_id=$3))`, messageID, id, m.AccountID).Scan(&viewLimit)
			if err != nil {
				writeJSON(w, 404, map[string]any{"ok": false, "error": "message not found"})
				return
			}
			if req.Pinned {
				_, _ = a.db.Pool().Exec(r.Context(), `UPDATE messages SET pinned_at=NOW(),pinned_by=$1,updated_at=NOW() WHERE id=$2`, m.AccountID, messageID)
			} else {
				tag, e := a.db.Pool().Exec(r.Context(), `UPDATE messages SET pinned_at=NULL,pinned_by=NULL,updated_at=NOW() WHERE id=$1 AND pinned_by=$2`, messageID, m.AccountID)
				if e != nil || tag.RowsAffected() == 0 {
					writeJSON(w, 403, map[string]any{"ok": false, "error": "only the member who pinned this message can remove the pin"})
					return
				}
			}
			pinPayload := map[string]any{"type": "message_pin", "conversation_id": id, "message_id": messageID, "pinned": req.Pinned, "pinned_by": func() int64 {
				if req.Pinned {
					return m.AccountID
				}
				return 0
			}()}
			a.sendMessageScopedPayload(r.Context(), id, messageID, viewLimit, pinPayload)
			writeJSON(w, 200, map[string]any{"ok": true, "pinned": req.Pinned})
			return
		}
	}
	writeJSON(w, 405, map[string]any{"ok": false})
}

func (a *appServer) conversationAccountIDs(ctx context.Context, conversationID int64) ([]int64, error) {
	rows, err := a.db.Pool().Query(ctx, `SELECT account_id FROM conversation_members WHERE conversation_id=$1`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

func (a *appServer) sendMessageScopedPayload(ctx context.Context, conversationID, messageID int64, viewLimit int, payload any) {
	if viewLimit <= 0 {
		a.chat.broadcast(conversationID, payload)
		return
	}
	// Restricted messages are delivered only to the sender and accounts that
	// already claimed a viewer slot. Never fall back to a room-wide broadcast.
	rows, err := a.db.Pool().Query(ctx, `SELECT account_id FROM message_views WHERE message_id=$1 UNION SELECT sender_account_id FROM messages WHERE id=$1 AND sender_account_id IS NOT NULL`, messageID)
	if err != nil {
		return
	}
	defer rows.Close()
	seen := map[int64]bool{}
	for rows.Next() {
		var accountID int64
		if rows.Scan(&accountID) != nil || accountID < 1 || seen[accountID] {
			continue
		}
		seen[accountID] = true
		a.chat.sendToConversationAccount(conversationID, accountID, payload)
	}
}

func (a *appServer) saveChatMessage(ctx context.Context, conversationID int64, m memberSessionEntry, payload chatSendPayload) (map[string]any, error) {
	body, viewLimit, ttlSeconds := parseMessageDirectives(payload.Body)
	if body == "" && len(payload.Attachments) == 0 {
		return nil, fmt.Errorf("message is empty")
	}
	if len(body) > 8000 {
		body = body[:8000]
	}
	if len(payload.Attachments) > 8 {
		payload.Attachments = payload.Attachments[:8]
	}
	payload.ClientNonce = strings.TrimSpace(payload.ClientNonce)
	if len(payload.ClientNonce) > 96 {
		payload.ClientNonce = payload.ClientNonce[:96]
	}

	var conversationType string
	var conversationDivisionID *int64
	if err := a.db.Pool().QueryRow(ctx, `SELECT conversation_type,division_id FROM conversations WHERE id=$1`, conversationID).Scan(&conversationType, &conversationDivisionID); err != nil {
		return nil, fmt.Errorf("conversation is unavailable")
	}
	if viewLimit > 0 && conversationType == "private" {
		return nil, fmt.Errorf("limited-view messages are available only in group chats")
	}

	var reply any
	if payload.ReplyTo > 0 {
		var belongs bool
		_ = a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM messages msg WHERE msg.id=$1 AND msg.conversation_id=$2 AND msg.deleted_at IS NULL AND (msg.expires_at IS NULL OR msg.expires_at>NOW()) AND (msg.view_limit IS NULL OR msg.sender_account_id=$3 OR EXISTS(SELECT 1 FROM message_views mv WHERE mv.message_id=msg.id AND mv.account_id=$3)))`, payload.ReplyTo, conversationID, m.AccountID).Scan(&belongs)
		if belongs {
			reply = payload.ReplyTo
		} else {
			payload.ReplyTo = 0
		}
	}
	raw, _ := json.Marshal(payload.Attachments)
	var id int64
	var created time.Time
	var err error
	var expires any
	if ttlSeconds > 0 {
		expires = time.Now().Add(time.Duration(ttlSeconds) * time.Second)
	}
	var view any
	if viewLimit > 0 {
		view = viewLimit
	}
	if payload.ClientNonce != "" {
		err = a.db.Pool().QueryRow(ctx, `INSERT INTO messages(conversation_id,sender_account_id,body,reply_to_id,attachments,client_nonce,view_limit,expires_at) VALUES($1,$2,NULLIF($3,''),$4,$5::jsonb,$6,$7,$8) ON CONFLICT (conversation_id,sender_account_id,client_nonce) WHERE client_nonce IS NOT NULL AND sender_account_id IS NOT NULL DO UPDATE SET updated_at=messages.updated_at RETURNING id,created_at`, conversationID, m.AccountID, body, reply, string(raw), payload.ClientNonce, view, expires).Scan(&id, &created)
		if isNonceSchemaCompatibilityError(err) {
			err = a.db.Pool().QueryRow(ctx, `INSERT INTO messages(conversation_id,sender_account_id,body,reply_to_id,attachments) VALUES($1,$2,NULLIF($3,''),$4,$5::jsonb) RETURNING id,created_at`, conversationID, m.AccountID, body, reply, string(raw)).Scan(&id, &created)
			viewLimit = 0
			ttlSeconds = 0
			expires = nil
		}
	} else {
		err = a.db.Pool().QueryRow(ctx, `INSERT INTO messages(conversation_id,sender_account_id,body,reply_to_id,attachments,view_limit,expires_at) VALUES($1,$2,NULLIF($3,''),$4,$5::jsonb,$6,$7) RETURNING id,created_at`, conversationID, m.AccountID, body, reply, string(raw), view, expires).Scan(&id, &created)
	}
	if err != nil {
		return nil, err
	}
	_, _ = a.db.Pool().Exec(ctx, `UPDATE conversations SET updated_at=NOW() WHERE id=$1`, conversationID)

	// Organization/community and division-group attachments belong in the shared Library.
	// Private DMs and ordinary private groups deliberately remain excluded.
	sharedConversation := conversationType == "community" || (conversationType == "group" && conversationDivisionID != nil)
	if sharedConversation {
		visibility := "organization"
		var division any
		if conversationDivisionID != nil {
			visibility = "division"
			division = *conversationDivisionID
		}
		for _, attachment := range payload.Attachments {
			if strings.TrimSpace(attachment.URL) == "" {
				continue
			}
			mediaType := attachment.Type
			if mediaType != "image" && mediaType != "video" && mediaType != "audio" {
				mediaType = "document"
			}
			var libraryID int64
			if a.db.Pool().QueryRow(ctx, `SELECT id FROM media_library WHERE url=$1 ORDER BY id DESC LIMIT 1`, attachment.URL).Scan(&libraryID) != nil {
				_ = a.db.Pool().QueryRow(ctx, `INSERT INTO media_library(label,media_type,url,visibility,division_id,uploaded_by,is_active,extension,location_label,reason,file_size,source_type) VALUES($1,$2,$3,$4,$5,$6,TRUE,$7,'Member chat','Shared organization/division chat attachment',$8,'member-chat') RETURNING id`, attachment.Name, mediaType, attachment.URL, visibility, division, m.AccountID, strings.TrimPrefix(strings.ToLower(filepath.Ext(attachment.Name)), "."), attachment.Size).Scan(&libraryID)
			} else {
				_, _ = a.db.Pool().Exec(ctx, `UPDATE media_library SET visibility=$1,division_id=$2,is_active=TRUE,updated_at=NOW() WHERE id=$3`, visibility, division, libraryID)
			}
			if libraryID > 0 {
				_, _ = a.db.Pool().Exec(ctx, `INSERT INTO media_library_usage(media_id,account_id,location_label,action,reason) SELECT $1,$2,'Member · Shared chat','shared','Organization/division chat attachment.' WHERE NOT EXISTS (SELECT 1 FROM media_library_usage WHERE media_id=$1 AND account_id=$2 AND location_label='Member · Shared chat' AND action='shared' AND reason='Organization/division chat attachment.')`, libraryID, m.AccountID)
			}
		}
	}
	message := map[string]any{"id": id, "sender_account_id": m.AccountID, "sender": m.Name, "avatar": m.Avatar, "body": body, "reply_to": payload.ReplyTo, "attachments": payload.Attachments, "time": formatPublicDateTime(created), "seen": false, "reactions": 0, "reaction_summary": map[string]int64{}, "client_nonce": payload.ClientNonce, "edited": false, "pinned": false, "pinned_by": int64(0), "view_limit": viewLimit, "view_count": 0}
	if ttlSeconds > 0 {
		if expiry, ok := expires.(time.Time); ok {
			message["expires_at"] = expiry.UTC().Format(time.RFC3339Nano)
		}
	}
	return message, nil
}

func (a *appServer) messageReactionSummary(ctx context.Context, messageID int64) (int64, map[string]int64) {
	rows, err := a.db.Pool().Query(ctx, `SELECT reaction,COUNT(*) FROM message_reactions WHERE message_id=$1 GROUP BY reaction ORDER BY MIN(created_at)`, messageID)
	if err != nil {
		return 0, map[string]int64{}
	}
	defer rows.Close()
	total := int64(0)
	summary := map[string]int64{}
	for rows.Next() {
		var reaction string
		var count int64
		if rows.Scan(&reaction, &count) == nil {
			summary[reaction] = count
			total += count
		}
	}
	return total, summary
}

func (a *appServer) memberUnreadNotificationCount(ctx context.Context, accountID int64) int64 {
	if a == nil || a.db == nil || accountID < 1 {
		return 0
	}
	var count int64
	_ = a.db.Pool().QueryRow(ctx, `SELECT COUNT(*) FROM notifications WHERE account_id=$1 AND read_at IS NULL`, accountID).Scan(&count)
	return count
}

func (a *appServer) memberChatInboxSocket(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		http.Error(w, "member login required", http.StatusUnauthorized)
		return
	}
	conn, err := a.chat.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	a.chat.addInbox(m.AccountID, conn)
	defer func() { a.chat.removeInbox(m.AccountID, conn); _ = conn.Close() }()
	conn.SetReadLimit(8 << 10)
	_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	a.chat.writeMu.Lock()
	_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_ = conn.WriteJSON(map[string]any{"type": "ready", "notification_unread": a.memberUnreadNotificationCount(r.Context(), m.AccountID)})
	a.chat.writeMu.Unlock()
	for {
		var msg struct {
			Type string `json:"type"`
		}
		if conn.ReadJSON(&msg) != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
		if msg.Type == "ping" {
			a.chat.writeMu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
			_ = conn.WriteJSON(map[string]any{"type": "pong", "notification_unread": a.memberUnreadNotificationCount(r.Context(), m.AccountID)})
			a.chat.writeMu.Unlock()
		}
	}
}

func (a *appServer) memberChatSocket(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		http.Error(w, "member login required", 401)
		return
	}
	conversationID, _ := strconv.ParseInt(r.URL.Query().Get("conversation_id"), 10, 64)
	if conversationID < 1 || !a.memberCanAccessConversation(r.Context(), m.AccountID, conversationID) {
		http.Error(w, "conversation unavailable", 404)
		return
	}
	conn, err := a.chat.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := chatClient{conn: conn, accountID: m.AccountID, name: m.Name, conversationID: conversationID}
	a.chat.add(client)
	var conversationType string
	_ = a.db.Pool().QueryRow(r.Context(), `SELECT conversation_type FROM conversations WHERE id=$1`, conversationID).Scan(&conversationType)
	defer func() {
		a.chat.remove(client)
		if payload, notify := a.chat.disconnectCall(conversationID, m.AccountID); payload != nil {
			a.chat.broadcast(conversationID, payload)
			if notify {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				if memberIDs, e := a.conversationAccountIDs(ctx, conversationID); e == nil {
					a.chat.notifyAccounts(memberIDs, payload)
				}
				cancel()
			}
		}
		_ = conn.Close()
	}()
	conn.SetReadLimit(2 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	a.broadcastSeen(r.Context(), conversationID, m.AccountID)
	for {
		var msg struct {
			Type            string           `json:"type"`
			Body            string           `json:"body"`
			Reaction        string           `json:"reaction"`
			ClientNonce     string           `json:"client_nonce"`
			CallID          string           `json:"call_id"`
			Mode            string           `json:"mode"`
			ReplyTo         int64            `json:"reply_to"`
			MessageID       int64            `json:"message_id"`
			TargetAccountID int64            `json:"target_account_id"`
			Attachments     []chatAttachment `json:"attachments"`
			Signal          json.RawMessage  `json:"signal"`
			Muted           bool             `json:"muted"`
			CameraOff       bool             `json:"camera_off"`
		}
		if conn.ReadJSON(&msg) != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(70 * time.Second))
		switch msg.Type {
		case "ping":
			a.chat.writeMu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
			_ = conn.WriteJSON(map[string]any{"type": "pong"})
			a.chat.writeMu.Unlock()
		case "typing", "typing_stop":
			a.chat.broadcast(conversationID, map[string]any{"type": msg.Type, "account_id": m.AccountID, "name": m.Name})
		case "seen":
			a.broadcastSeen(r.Context(), conversationID, m.AccountID)
		case "react":
			if msg.MessageID > 0 {
				var viewLimit int
				if err := a.db.Pool().QueryRow(r.Context(), `SELECT COALESCE(m.view_limit,0) FROM messages m WHERE m.id=$1 AND m.conversation_id=$2 AND m.deleted_at IS NULL AND m.unsent_at IS NULL AND (m.expires_at IS NULL OR m.expires_at>NOW()) AND (m.view_limit IS NULL OR m.sender_account_id=$3 OR EXISTS(SELECT 1 FROM message_views mv WHERE mv.message_id=m.id AND mv.account_id=$3))`, msg.MessageID, conversationID, m.AccountID).Scan(&viewLimit); err != nil {
					continue
				}
				reaction := strings.TrimSpace(msg.Reaction)
				if reaction == "" || len([]rune(reaction)) > 8 {
					reaction = "👍"
				}
				_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO message_reactions(message_id,account_id,reaction,updated_at) VALUES($1,$2,$3,NOW()) ON CONFLICT (message_id,account_id) DO UPDATE SET reaction=EXCLUDED.reaction,updated_at=NOW()`, msg.MessageID, m.AccountID, reaction)
				count, summary := a.messageReactionSummary(r.Context(), msg.MessageID)
				a.sendMessageScopedPayload(r.Context(), conversationID, msg.MessageID, viewLimit, map[string]any{"type": "reaction", "message_id": msg.MessageID, "count": count, "summary": summary, "account_id": m.AccountID, "reaction": reaction})
			}
		case "send":
			message, err := a.saveChatMessage(r.Context(), conversationID, m, chatSendPayload{Body: msg.Body, ReplyTo: msg.ReplyTo, Attachments: msg.Attachments, ClientNonce: msg.ClientNonce})
			if err != nil {
				continue
			}
			if limit, _ := message["view_limit"].(int); limit > 0 {
				a.chat.broadcast(conversationID, map[string]any{"type": "message_available", "conversation_id": conversationID, "message_id": message["id"]})
			} else {
				a.chat.broadcast(conversationID, map[string]any{"type": "message", "message": message})
			}
			if memberIDs, e := a.conversationAccountIDs(r.Context(), conversationID); e == nil {
				a.chat.notifyAccounts(memberIDs, map[string]any{"type": "conversation_update", "conversation_id": conversationID, "message_id": message["id"], "sender_account_id": m.AccountID})
			}
		case "call_start":
			callID := strings.TrimSpace(msg.CallID)
			mode := strings.ToLower(strings.TrimSpace(msg.Mode))
			if callID == "" || len(callID) > 96 {
				continue
			}
			if mode != "audio" && mode != "video" {
				mode = "audio"
			}
			room, created := a.chat.startCall(conversationID, m.AccountID, callID, mode)
			if !created {
				a.chat.writeMu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
				_ = conn.WriteJSON(map[string]any{"type": "call_active", "conversation_id": conversationID, "call_id": room.CallID, "mode": room.Mode, "host_account_id": room.HostAccountID, "group": conversationType != "private", "participant_count": len(room.Participants)})
				a.chat.writeMu.Unlock()
				continue
			}
			payload := map[string]any{"type": "call_start", "conversation_id": conversationID, "call_id": callID, "mode": mode, "from_account_id": m.AccountID, "from_name": m.Name, "from_avatar": m.Avatar, "host_account_id": m.AccountID, "group": conversationType != "private", "participant_count": 1}
			a.chat.broadcast(conversationID, payload)
			if memberIDs, e := a.conversationAccountIDs(r.Context(), conversationID); e == nil {
				others := make([]int64, 0, len(memberIDs))
				for _, accountID := range memberIDs {
					if accountID != m.AccountID {
						others = append(others, accountID)
					}
				}
				a.chat.notifyAccounts(others, payload)
			}
		case "call_join":
			callID := strings.TrimSpace(msg.CallID)
			room, joined := a.chat.joinCall(conversationID, m.AccountID, callID)
			if !joined {
				continue
			}
			payload := map[string]any{"type": "call_join", "conversation_id": conversationID, "call_id": callID, "account_id": m.AccountID, "name": m.Name, "avatar": m.Avatar, "host_account_id": room.HostAccountID, "group": conversationType != "private", "participant_count": len(room.Participants)}
			a.chat.broadcast(conversationID, payload)
		case "call_leave":
			callID := strings.TrimSpace(msg.CallID)
			ended, room := a.chat.leaveCall(conversationID, m.AccountID, callID)
			if room == nil {
				continue
			}
			eventType := "call_leave"
			if ended {
				eventType = "call_end"
			}
			payload := map[string]any{"type": eventType, "conversation_id": conversationID, "call_id": callID, "account_id": m.AccountID, "name": m.Name, "avatar": m.Avatar, "host_account_id": room.HostAccountID, "group": conversationType != "private", "participant_count": len(room.Participants)}
			a.chat.broadcast(conversationID, payload)
			if ended {
				if memberIDs, e := a.conversationAccountIDs(r.Context(), conversationID); e == nil {
					a.chat.notifyAccounts(memberIDs, payload)
				}
			}
		case "call_end":
			callID := strings.TrimSpace(msg.CallID)
			room, ended := a.chat.endCall(conversationID, m.AccountID, callID, conversationType == "private")
			if room == nil {
				continue
			}
			eventType := "call_leave"
			if ended {
				eventType = "call_end"
			}
			payload := map[string]any{"type": eventType, "conversation_id": conversationID, "call_id": callID, "account_id": m.AccountID, "name": m.Name, "avatar": m.Avatar, "host_account_id": room.HostAccountID, "group": conversationType != "private", "participant_count": len(room.Participants)}
			a.chat.broadcast(conversationID, payload)
			if ended {
				if memberIDs, e := a.conversationAccountIDs(r.Context(), conversationID); e == nil {
					a.chat.notifyAccounts(memberIDs, payload)
				}
			}
		case "call_state":
			callID := strings.TrimSpace(msg.CallID)
			room := a.chat.callRoom(conversationID)
			if room == nil || room.CallID != callID || !room.Participants[m.AccountID] {
				continue
			}
			a.chat.broadcast(conversationID, map[string]any{"type": "call_state", "conversation_id": conversationID, "call_id": callID, "account_id": m.AccountID, "muted": msg.Muted, "camera_off": msg.CameraOff})
		case "call_signal":
			callID := strings.TrimSpace(msg.CallID)
			if callID == "" || msg.TargetAccountID < 1 || len(msg.Signal) == 0 {
				continue
			}
			room := a.chat.callRoom(conversationID)
			if room == nil || room.CallID != callID || !room.Participants[m.AccountID] || !room.Participants[msg.TargetAccountID] {
				continue
			}
			payload := map[string]any{"type": "call_signal", "conversation_id": conversationID, "call_id": callID, "from_account_id": m.AccountID, "target_account_id": msg.TargetAccountID, "signal": json.RawMessage(msg.Signal)}
			// SDP/ICE is private signaling. Deliver it only to the target account's
			// sockets in this conversation; never broadcast peer negotiation data
			// to other room participants.
			a.chat.sendToConversationAccount(conversationID, msg.TargetAccountID, payload)
		}
	}
}
