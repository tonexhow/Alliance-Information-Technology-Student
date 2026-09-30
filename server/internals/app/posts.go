package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func isNonceSchemaCompatibilityError(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 42703 = undefined_column, 42P10 = invalid_column_reference
		// (the latter is PostgreSQL's error when an ON CONFLICT target has
		// no matching unique/exclusion constraint yet).
		return pgErr.Code == "42703" || pgErr.Code == "42P10"
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "client_nonce") && (strings.Contains(text, "does not exist") || strings.Contains(text, "no unique or exclusion constraint"))
}

type postActor struct {
	AccountID int64
	Member    *memberSessionEntry
	Visitor   *sessionEntry
}

func (a *appServer) actorForRequest(r *http.Request) postActor {
	if m, ok := a.currentMember(r); ok {
		return postActor{AccountID: m.AccountID, Member: &m}
	}
	if s, ok := a.currentOAuthSession(r); ok {
		return postActor{AccountID: s.AccountID, Visitor: &s}
	}
	return postActor{}
}

func (a *appServer) recordPostAnalytics(ctx context.Context, actor postActor, eventType string, postID int64) {
	if strings.TrimSpace(eventType) == "" {
		return
	}
	kind := "anonymous"
	var account any
	if actor.AccountID > 0 {
		account = actor.AccountID
		if actor.Member != nil {
			kind = "member"
		} else {
			kind = "visitor"
		}
	}
	_, _ = a.db.Pool().Exec(ctx, `INSERT INTO analytics_events(account_id,visitor_kind,event_type,entity_type,entity_id) VALUES($1,$2,$3,'post',$4)`, account, kind, eventType, strconv.FormatInt(postID, 10))
}

func (a *appServer) canReadPost(ctx context.Context, postID int64, actor postActor) (string, int64, bool, error) {
	var visibility, moderation string
	var divisionID sql.NullInt64
	var authorID int64
	err := a.db.Pool().QueryRow(ctx, `SELECT visibility,division_id,moderation_status,author_account_id FROM posts WHERE id=$1 AND deleted_at IS NULL`, postID).Scan(&visibility, &divisionID, &moderation, &authorID)
	if err != nil {
		return "", 0, false, err
	}
	if moderation != "published" {
		return visibility, divisionID.Int64, actor.AccountID > 0 && actor.AccountID == authorID, nil
	}
	if visibility == "public" {
		return visibility, divisionID.Int64, true, nil
	}
	if actor.Member == nil {
		return visibility, divisionID.Int64, false, nil
	}
	if visibility == "organization" {
		return visibility, divisionID.Int64, true, nil
	}
	return visibility, divisionID.Int64, divisionID.Valid && actor.Member.DivisionID == divisionID.Int64, nil
}

func (a *appServer) postRouter(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, 503, map[string]any{"ok": false, "error": "AITS services are temporarily unavailable"})
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/posts/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	postID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || postID < 1 {
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			a.getPostDetail(w, r, postID)
		case http.MethodPatch:
			a.editMemberPost(w, r, postID)
		case http.MethodDelete:
			a.deleteMemberPost(w, r, postID)
		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
		}
		return
	}
	if len(parts) == 2 && parts[1] == "history" && r.Method == http.MethodGet {
		a.memberPostHistory(w, r, postID)
		return
	}
	if len(parts) == 2 && parts[1] == "reaction" && r.Method == http.MethodPost {
		a.togglePostReactionAPI(w, r, postID)
		return
	}
	if len(parts) == 2 && parts[1] == "comments" && r.Method == http.MethodPost {
		a.addPostComment(w, r, postID)
		return
	}
	if len(parts) == 3 && parts[1] == "comments" {
		commentID, e := strconv.ParseInt(parts[2], 10, 64)
		if e == nil {
			switch r.Method {
			case http.MethodPost:
				a.toggleCommentReactionAPI(w, r, postID, commentID)
			case http.MethodPatch:
				a.editPostComment(w, r, postID, commentID)
			case http.MethodDelete:
				a.deletePostComment(w, r, postID, commentID)
			default:
				writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false})
			}
			return
		}
	}
	if len(parts) == 4 && parts[1] == "comments" && parts[3] == "history" && r.Method == http.MethodGet {
		commentID, e := strconv.ParseInt(parts[2], 10, 64)
		if e == nil {
			a.postCommentHistory(w, r, postID, commentID)
			return
		}
	}
	writeJSON(w, 404, map[string]any{"ok": false, "error": "route not found"})
}

func normalizeSocialReaction(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "❤️"
	}
	switch value {
	case "👍", "❤️", "😂", "🎉", "😮", "😢", "😡", "👏", "🙏", "🔥", "💯", "👀":
		return value, true
	default:
		return "", false
	}
}

func requestedSocialReaction(r *http.Request) (string, bool) {
	var req struct {
		Reaction string `json:"reaction"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 2048)).Decode(&req)
	}
	return normalizeSocialReaction(req.Reaction)
}

func (a *appServer) postReactionSummary(ctx context.Context, postID int64) map[string]int64 {
	summary := map[string]int64{}
	rows, err := a.db.Pool().Query(ctx, `SELECT reaction,COUNT(*) FROM post_reactions WHERE post_id=$1 GROUP BY reaction ORDER BY COUNT(*) DESC,reaction`, postID)
	if err != nil {
		return summary
	}
	defer rows.Close()
	for rows.Next() {
		var reaction string
		var count int64
		if rows.Scan(&reaction, &count) == nil {
			summary[reaction] = count
		}
	}
	return summary
}

func (a *appServer) commentReactionSummary(ctx context.Context, commentID int64) map[string]int64 {
	summary := map[string]int64{}
	rows, err := a.db.Pool().Query(ctx, `SELECT reaction,COUNT(*) FROM comment_reactions WHERE comment_id=$1 GROUP BY reaction ORDER BY COUNT(*) DESC,reaction`, commentID)
	if err != nil {
		return summary
	}
	defer rows.Close()
	for rows.Next() {
		var reaction string
		var count int64
		if rows.Scan(&reaction, &count) == nil {
			summary[reaction] = count
		}
	}
	return summary
}

func (a *appServer) getPostDetail(w http.ResponseWriter, r *http.Request, postID int64) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	actor := a.actorForRequest(r)
	visibility, _, allowed, err := a.canReadPost(ctx, postID, actor)
	if err != nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "post not found"})
		return
	}
	if !allowed {
		status := http.StatusForbidden
		if actor.Member == nil {
			status = http.StatusUnauthorized
		}
		writeJSON(w, status, map[string]any{"ok": false, "error": "member login required for this post", "member_required": true, "visibility": visibility})
		return
	}
	var id int64
	var pinned bool
	var author, role, title, body, mediaJSON string
	var published time.Time
	var divisionName sql.NullString
	var reactions, comments int64
	var identity, avatar, divisionLogo, postedBy string
	err = a.db.Pool().QueryRow(ctx, `SELECT p.id,p.is_pinned,CASE WHEN COALESCE(p.identity_kind,'member')='member' THEN a.display_name ELSE COALESCE(NULLIF(p.author_label,''),a.display_name) END,COALESCE(NULLIF(p.role_label,''),'Member'),COALESCE(p.title,''),p.body,p.media::text,COALESCE(p.published_at,p.created_at),d.name,(SELECT COUNT(*) FROM post_reactions pr WHERE pr.post_id=p.id),(SELECT COUNT(*) FROM post_comments pc WHERE pc.post_id=p.id AND pc.status='visible'),COALESCE(p.identity_kind,'member'),COALESCE(a.avatar_url,''),COALESCE(d.logo_url,''),a.display_name FROM posts p JOIN accounts a ON a.id=p.author_account_id LEFT JOIN divisions d ON d.id=p.division_id WHERE p.id=$1 AND p.deleted_at IS NULL`, postID).Scan(&id, &pinned, &author, &role, &title, &body, &mediaJSON, &published, &divisionName, &reactions, &comments, &identity, &avatar, &divisionLogo, &postedBy)
	if err != nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "post not found"})
		return
	}
	media := make([]map[string]any, 0)
	_ = json.Unmarshal([]byte(mediaJSON), &media)
	reactors := make([]string, 0)
	if reactionRows, reactionErr := a.db.Pool().Query(ctx, `SELECT ac.display_name FROM post_reactions pr JOIN accounts ac ON ac.id=pr.account_id WHERE pr.post_id=$1 ORDER BY pr.created_at DESC LIMIT 50`, postID); reactionErr == nil {
		for reactionRows.Next() {
			var name string
			if reactionRows.Scan(&name) == nil {
				reactors = append(reactors, name)
			}
		}
		reactionRows.Close()
	}
	postReactionSummary := a.postReactionSummary(ctx, postID)
	myPostReaction := ""
	if actor.AccountID > 0 {
		_ = a.db.Pool().QueryRow(ctx, `SELECT reaction FROM post_reactions WHERE post_id=$1 AND account_id=$2`, postID, actor.AccountID).Scan(&myPostReaction)
	}
	reacted := myPostReaction != ""
	rows, err := a.db.Pool().Query(ctx, `SELECT c.id,c.parent_comment_id,c.author_account_id,ac.display_name,COALESCE(ac.avatar_url,''),c.body,c.created_at,c.edited_at,(SELECT COUNT(*) FROM comment_reactions cr WHERE cr.comment_id=c.id),CASE WHEN $2::bigint>0 THEN COALESCE((SELECT cr.reaction FROM comment_reactions cr WHERE cr.comment_id=c.id AND cr.account_id=$2 LIMIT 1),'') ELSE '' END,ac.account_kind,EXISTS(SELECT 1 FROM applications ap WHERE ap.account_id=ac.id AND ap.status NOT IN ('cancelled','declined')),(SELECT COUNT(*) FROM post_comment_history h WHERE h.comment_id=c.id),COALESCE(d.name,''),COALESCE((SELECT r.name FROM account_roles ar JOIN roles r ON r.id=ar.role_id WHERE ar.account_id=ac.id AND ar.is_active=TRUE AND (ar.expires_at IS NULL OR ar.expires_at>NOW()) ORDER BY r.hierarchy_level DESC LIMIT 1),CASE WHEN ac.account_kind='member' THEN 'Member' ELSE 'Visitor' END),COALESCE(mp.membership_level,'') FROM post_comments c JOIN accounts ac ON ac.id=c.author_account_id LEFT JOIN member_profiles mp ON mp.account_id=ac.id LEFT JOIN divisions d ON d.id=mp.primary_division_id WHERE c.post_id=$1 AND c.status='visible' ORDER BY c.created_at,c.id`, postID, actor.AccountID)
	items := make([]map[string]any, 0)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var cid, authorAccountID, historyCount int64
			var parent sql.NullInt64
			var name, avatar, cbody, division, access, membership string
			var created time.Time
			var edited *time.Time
			var count int64
			var myReaction string
			var accountKind string
			var applicant bool
			if rows.Scan(&cid, &parent, &authorAccountID, &name, &avatar, &cbody, &created, &edited, &count, &myReaction, &accountKind, &applicant, &historyCount, &division, &access, &membership) == nil {
				var parentVal any = nil
				if parent.Valid {
					parentVal = parent.Int64
				}
				tag := ""
				if accountKind == "visitor" {
					if applicant {
						tag = "APPLICANT"
					} else {
						tag = "VISITOR"
					}
				}
				items = append(items, map[string]any{"id": cid, "parent_id": parentVal, "account_id": authorAccountID, "name": name, "avatar": avatar, "body": cbody, "time": formatPublicDateTime(created), "edited": edited != nil, "edited_time": func() string {
					if edited == nil {
						return ""
					}
					return formatPublicDateTime(*edited)
				}(), "history_count": historyCount, "can_edit": actor.AccountID == authorAccountID, "reactions": count, "reacted_by_me": myReaction != "", "my_reaction": myReaction, "reaction_summary": a.commentReactionSummary(ctx, cid), "tag": tag, "account_kind": accountKind, "division": division, "access": access, "membership": membership})
			}
		}
	}
	var postOwner int64
	var editedAt *time.Time
	var historyCount int64
	_ = a.db.Pool().QueryRow(ctx, `SELECT author_account_id,edited_at,(SELECT COUNT(*) FROM post_history h WHERE h.post_id=posts.id AND h.created_at>NOW()-INTERVAL '1 month') FROM posts WHERE id=$1 AND deleted_at IS NULL`, postID).Scan(&postOwner, &editedAt, &historyCount)
	writeJSON(w, 200, map[string]any{"ok": true, "post": map[string]any{"id": id, "pinned": pinned, "author": author, "role": role, "date": formatPublicDateTime(published), "title": title, "body": body, "media": media, "reactions": reactions, "comments": comments, "visibility": visibility, "division": divisionName.String, "reacted_by_me": reacted, "my_reaction": myPostReaction, "reaction_summary": postReactionSummary, "reacted_by": reactors, "comment_items": items, "identity": identity, "avatar": avatar, "division_logo": divisionLogo, "posted_by": postedBy, "edited": editedAt != nil, "history_count": historyCount, "can_edit": actor.AccountID > 0 && actor.AccountID == postOwner}})
}

func (a *appServer) requireInteractionActor(w http.ResponseWriter, r *http.Request, postID int64) (postActor, bool) {
	actor := a.actorForRequest(r)
	if actor.AccountID == 0 {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "sign in required"})
		return actor, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	visibility, _, allowed, err := a.canReadPost(ctx, postID, actor)
	if err != nil {
		writeJSON(w, 404, map[string]any{"ok": false})
		return actor, false
	}
	if !allowed {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "this post is limited to AITS members", "member_required": visibility != "public"})
		return actor, false
	}
	return actor, true
}

func (a *appServer) togglePostReactionAPI(w http.ResponseWriter, r *http.Request, postID int64) {
	actor, ok := a.requireInteractionActor(w, r, postID)
	if !ok {
		return
	}
	reaction, validReaction := requestedSocialReaction(r)
	if !validReaction {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "unsupported reaction"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	current := ""
	_ = a.db.Pool().QueryRow(ctx, `SELECT reaction FROM post_reactions WHERE post_id=$1 AND account_id=$2`, postID, actor.AccountID).Scan(&current)
	reacted := true
	myReaction := reaction
	created := current == ""
	if current == reaction {
		_, _ = a.db.Pool().Exec(ctx, `DELETE FROM post_reactions WHERE post_id=$1 AND account_id=$2`, postID, actor.AccountID)
		reacted = false
		myReaction = ""
	} else {
		_, _ = a.db.Pool().Exec(ctx, `INSERT INTO post_reactions(post_id,account_id,reaction) VALUES($1,$2,$3) ON CONFLICT(post_id,account_id) DO UPDATE SET reaction=EXCLUDED.reaction,updated_at=NOW()`, postID, actor.AccountID, reaction)
	}
	summary := a.postReactionSummary(ctx, postID)
	var count int64
	for _, n := range summary {
		count += n
	}
	if created && reacted {
		var owner int64
		var title string
		if a.db.Pool().QueryRow(ctx, `SELECT author_account_id,COALESCE(NULLIF(title,''),'your post') FROM posts WHERE id=$1`, postID).Scan(&owner, &title) == nil && owner > 0 && owner != actor.AccountID {
			var isMember bool
			_ = a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member_profiles WHERE account_id=$1)`, owner).Scan(&isMember)
			if isMember {
				_, _ = a.db.Pool().Exec(ctx, `INSERT INTO notifications(account_id,notification_type,title,body,action_url,target_post_id) VALUES($1,'post','New reaction on '||$2,'Someone reacted to your post.',$3,$4)`, owner, title, fmt.Sprintf("/member/posts/%d", postID), postID)
				a.notifyMemberNotification(owner)
			}
		}
	}
	a.recordPostAnalytics(ctx, actor, "post_react", postID)
	writeJSON(w, 200, map[string]any{"ok": true, "reacted": reacted, "my_reaction": myReaction, "count": count, "summary": summary})
}

func (a *appServer) addPostComment(w http.ResponseWriter, r *http.Request, postID int64) {
	actor, ok := a.requireInteractionActor(w, r, postID)
	if !ok {
		return
	}
	var req struct {
		Body        string `json:"body"`
		ParentID    *int64 `json:"parent_id"`
		ClientNonce string `json:"client_nonce"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req) != nil {
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}
	req.Body = strings.TrimSpace(req.Body)
	req.ClientNonce = strings.TrimSpace(req.ClientNonce)
	if len(req.ClientNonce) > 96 {
		req.ClientNonce = req.ClientNonce[:96]
	}
	if req.Body == "" || len(req.Body) > 2000 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "comment must contain 1-2000 characters"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if req.ParentID != nil {
		var valid bool
		_ = a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM post_comments WHERE id=$1 AND post_id=$2 AND status='visible')`, *req.ParentID, postID).Scan(&valid)
		if !valid {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "reply target not found"})
			return
		}
	}
	var id int64
	var err error
	if req.ClientNonce != "" {
		err = a.db.Pool().QueryRow(ctx, `INSERT INTO post_comments(post_id,author_account_id,parent_comment_id,body,client_nonce) VALUES($1,$2,$3,$4,$5) ON CONFLICT (author_account_id,client_nonce) WHERE client_nonce IS NOT NULL DO UPDATE SET updated_at=post_comments.updated_at RETURNING id`, postID, actor.AccountID, req.ParentID, req.Body, req.ClientNonce).Scan(&id)
		if isNonceSchemaCompatibilityError(err) {
			log.Printf("[COMMENT] nonce schema is not ready; retrying compatibility insert account=%d post=%d: %v", actor.AccountID, postID, err)
			err = a.db.Pool().QueryRow(ctx, `INSERT INTO post_comments(post_id,author_account_id,parent_comment_id,body) VALUES($1,$2,$3,$4) RETURNING id`, postID, actor.AccountID, req.ParentID, req.Body).Scan(&id)
		}
	} else {
		err = a.db.Pool().QueryRow(ctx, `INSERT INTO post_comments(post_id,author_account_id,parent_comment_id,body) VALUES($1,$2,$3,$4) RETURNING id`, postID, actor.AccountID, req.ParentID, req.Body).Scan(&id)
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "could not save comment"})
		return
	}
	a.recordPostAnalytics(ctx, actor, "post_comment", postID)
	// Notify a member post owner or the author of the comment being replied to.
	var targetAccountID int64
	var postTitle string
	_ = a.db.Pool().QueryRow(ctx, `SELECT author_account_id,COALESCE(NULLIF(title,''),'your post') FROM posts WHERE id=$1`, postID).Scan(&targetAccountID, &postTitle)
	if req.ParentID != nil {
		var replyAuthor int64
		if a.db.Pool().QueryRow(ctx, `SELECT author_account_id FROM post_comments WHERE id=$1`, *req.ParentID).Scan(&replyAuthor) == nil {
			targetAccountID = replyAuthor
		}
	}
	if targetAccountID > 0 && targetAccountID != actor.AccountID {
		var isMember bool
		_ = a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member_profiles WHERE account_id=$1)`, targetAccountID).Scan(&isMember)
		if isMember {
			title := "New comment on " + postTitle
			body := "Someone commented on your post."
			if req.ParentID != nil {
				title = "New reply on " + postTitle
				body = "Someone replied to your comment."
			}
			_, _ = a.db.Pool().Exec(ctx, `INSERT INTO notifications(account_id,notification_type,title,body,action_url,target_post_id,target_comment_id) VALUES($1,'post',$2,$3,$4,$5,$6)`, targetAccountID, title, body, fmt.Sprintf("/member/posts/%d?comment=%d", postID, id), postID, id)
			a.notifyMemberNotification(targetAccountID)
		}
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": id})
}

func (a *appServer) editPostComment(w http.ResponseWriter, r *http.Request, postID, commentID int64) {
	actor, ok := a.requireInteractionActor(w, r, postID)
	if !ok {
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req) != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid comment update"})
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 2000 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "comment must contain 1-2000 characters"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var owner int64
	var oldBody string
	var status string
	if err := a.db.Pool().QueryRow(ctx, `SELECT author_account_id,body,status FROM post_comments WHERE id=$1 AND post_id=$2`, commentID, postID).Scan(&owner, &oldBody, &status); err != nil || status != "visible" {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "comment not found"})
		return
	}
	if owner != actor.AccountID {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "you can only edit your own comment"})
		return
	}
	if body == oldBody {
		writeJSON(w, 200, map[string]any{"ok": true, "unchanged": true})
		return
	}
	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "comment could not be updated"})
		return
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO post_comment_history(comment_id,editor_account_id,body) VALUES($1,$2,$3)`, commentID, actor.AccountID, oldBody); err == nil {
		_, err = tx.Exec(ctx, `UPDATE post_comments SET body=$1,edited_at=NOW(),updated_at=NOW() WHERE id=$2`, body, commentID)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "comment could not be updated"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *appServer) deletePostComment(w http.ResponseWriter, r *http.Request, postID, commentID int64) {
	actor, ok := a.requireInteractionActor(w, r, postID)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var owner int64
	var body, status string
	if err := a.db.Pool().QueryRow(ctx, `SELECT author_account_id,body,status FROM post_comments WHERE id=$1 AND post_id=$2`, commentID, postID).Scan(&owner, &body, &status); err != nil || status != "visible" {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "comment not found"})
		return
	}
	if owner != actor.AccountID {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "you can only delete your own comment"})
		return
	}
	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "comment could not be deleted"})
		return
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO post_comment_history(comment_id,editor_account_id,body) VALUES($1,$2,$3)`, commentID, actor.AccountID, body); err == nil {
		_, err = tx.Exec(ctx, `UPDATE post_comments SET status='deleted',updated_at=NOW() WHERE id=$1`, commentID)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "comment could not be deleted"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *appServer) postCommentHistory(w http.ResponseWriter, r *http.Request, postID, commentID int64) {
	actor, ok := a.requireInteractionActor(w, r, postID)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var owner int64
	if err := a.db.Pool().QueryRow(ctx, `SELECT author_account_id FROM post_comments WHERE id=$1 AND post_id=$2`, commentID, postID).Scan(&owner); err != nil {
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	rows, err := a.db.Pool().Query(ctx, `SELECT h.id,h.body,h.created_at,a.display_name FROM post_comment_history h JOIN accounts a ON a.id=h.editor_account_id WHERE h.comment_id=$1 ORDER BY h.id DESC LIMIT 50`, commentID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "history unavailable"})
		return
	}
	defer rows.Close()
	history := []map[string]any{}
	for rows.Next() {
		var id int64
		var body, name string
		var created time.Time
		if rows.Scan(&id, &body, &created, &name) == nil {
			history = append(history, map[string]any{"id": id, "body": body, "time": formatPublicDateTime(created), "editor": name})
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "history": history, "can_edit": owner == actor.AccountID})
}

func (a *appServer) toggleCommentReactionAPI(w http.ResponseWriter, r *http.Request, postID, commentID int64) {
	actor, ok := a.requireInteractionActor(w, r, postID)
	if !ok {
		return
	}
	reaction, validReaction := requestedSocialReaction(r)
	if !validReaction {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "unsupported reaction"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var valid bool
	_ = a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM post_comments WHERE id=$1 AND post_id=$2 AND status='visible')`, commentID, postID).Scan(&valid)
	if !valid {
		writeJSON(w, 404, map[string]any{"ok": false})
		return
	}
	current := ""
	_ = a.db.Pool().QueryRow(ctx, `SELECT reaction FROM comment_reactions WHERE comment_id=$1 AND account_id=$2`, commentID, actor.AccountID).Scan(&current)
	reacted := true
	myReaction := reaction
	created := current == ""
	if current == reaction {
		_, _ = a.db.Pool().Exec(ctx, `DELETE FROM comment_reactions WHERE comment_id=$1 AND account_id=$2`, commentID, actor.AccountID)
		reacted = false
		myReaction = ""
	} else {
		_, _ = a.db.Pool().Exec(ctx, `INSERT INTO comment_reactions(comment_id,account_id,reaction) VALUES($1,$2,$3) ON CONFLICT(comment_id,account_id) DO UPDATE SET reaction=EXCLUDED.reaction,updated_at=NOW()`, commentID, actor.AccountID, reaction)
	}
	summary := a.commentReactionSummary(ctx, commentID)
	var count int64
	for _, n := range summary {
		count += n
	}
	if created && reacted {
		var owner int64
		if a.db.Pool().QueryRow(ctx, `SELECT author_account_id FROM post_comments WHERE id=$1`, commentID).Scan(&owner) == nil && owner > 0 && owner != actor.AccountID {
			var isMember bool
			_ = a.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member_profiles WHERE account_id=$1)`, owner).Scan(&isMember)
			if isMember {
				_, _ = a.db.Pool().Exec(ctx, `INSERT INTO notifications(account_id,notification_type,title,body,action_url,target_post_id,target_comment_id) VALUES($1,'post','New reaction on your comment','Someone reacted to your comment.',$2,$3,$4)`, owner, fmt.Sprintf("/member/posts/%d?comment=%d", postID, commentID), postID, commentID)
				a.notifyMemberNotification(owner)
			}
		}
	}
	a.recordPostAnalytics(ctx, actor, "post_comment", postID)
	writeJSON(w, 200, map[string]any{"ok": true, "reacted": reacted, "my_reaction": myReaction, "count": count, "summary": summary})
}

func (a *appServer) memberFeedV2(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	limit := 10
	if n, e := strconv.Atoi(r.URL.Query().Get("limit")); e == nil && n > 0 {
		if n > 25 {
			n = 25
		}
		limit = n
	}
	var before int64
	if n, e := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); e == nil {
		before = n
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	rows, err := a.db.Pool().Query(ctx, `SELECT p.id,CASE WHEN COALESCE(p.identity_kind,'member')='member' THEN a.display_name ELSE COALESCE(NULLIF(p.author_label,''),a.display_name) END,COALESCE(NULLIF(p.role_label,''),'Member'),COALESCE(p.published_at,p.created_at),COALESCE(p.title,''),p.body,p.media::text,(SELECT COUNT(*) FROM post_reactions pr WHERE pr.post_id=p.id),(SELECT COUNT(*) FROM post_comments pc WHERE pc.post_id=p.id AND pc.status='visible'),p.visibility,COALESCE(d.name,''),p.moderation_status,COALESCE(p.identity_kind,'member'),COALESCE(a.avatar_url,''),COALESCE(d.logo_url,''),a.display_name,p.edited_at,(SELECT COUNT(*) FROM post_history h WHERE h.post_id=p.id AND h.created_at>NOW()-INTERVAL '1 month'),p.author_account_id=$1 FROM posts p JOIN accounts a ON a.id=p.author_account_id LEFT JOIN divisions d ON d.id=p.division_id WHERE p.deleted_at IS NULL AND ((p.moderation_status='published' AND (p.visibility='public' OR p.visibility='organization' OR (p.visibility='division' AND p.division_id=$2))) OR (p.moderation_status='pending' AND p.author_account_id=$1)) AND ($3::bigint=0 OR p.id<$3) ORDER BY p.id DESC LIMIT $4`, m.AccountID, m.DivisionID, before, limit+1)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "could not load feed"})
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit+1)
	for rows.Next() {
		var id, reactions, comments int64
		var author, role, title, body, mediaJSON, visibility, division, status, identity, avatar, divisionLogo, postedBy string
		var t time.Time
		var editedAt *time.Time
		var historyCount int64
		var canEdit bool
		if rows.Scan(&id, &author, &role, &t, &title, &body, &mediaJSON, &reactions, &comments, &visibility, &division, &status, &identity, &avatar, &divisionLogo, &postedBy, &editedAt, &historyCount, &canEdit) != nil {
			continue
		}
		var media []map[string]any
		_ = json.Unmarshal([]byte(mediaJSON), &media)
		items = append(items, map[string]any{"id": id, "author": author, "role": role, "time": formatPublicDateTime(t), "title": title, "text": body, "media": media, "reactions": reactions, "comments": comments, "visibility": visibility, "division": division, "status": status, "identity": identity, "avatar": avatar, "division_logo": divisionLogo, "posted_by": postedBy, "edited": editedAt != nil, "history_count": historyCount, "can_edit": canEdit})
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var cursor any = nil
	if hasMore && len(items) > 0 {
		cursor = items[len(items)-1]["id"]
	}
	writeJSON(w, 200, map[string]any{"posts": items, "has_more": hasMore, "next_cursor": cursor})
}

func (a *appServer) editMemberPost(w http.ResponseWriter, r *http.Request, postID int64) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	var req struct{ Title, Body, Visibility, Identity string }
	if json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req) != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid post update"})
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Body = strings.TrimSpace(req.Body)
	req.Visibility = strings.ToLower(strings.TrimSpace(req.Visibility))
	req.Identity = strings.ToLower(strings.TrimSpace(req.Identity))
	if req.Body == "" || len(req.Body) > 10000 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "post text is required"})
		return
	}
	if req.Visibility != "public" && req.Visibility != "organization" && req.Visibility != "division" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid audience"})
		return
	}
	if req.Identity != "member" && req.Identity != "organization" && req.Identity != "division" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid post identity"})
		return
	}
	canBrand := strings.EqualFold(strings.TrimSpace(m.MembershipLevel), "Officer") || !strings.EqualFold(strings.TrimSpace(m.Role), "member")
	if req.Identity != "member" && !canBrand {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "that post identity is not available to this account"})
		return
	}
	if (req.Visibility == "division" || req.Identity == "division") && m.DivisionID == 0 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "no division is assigned to this member"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var owner int64
	var oldTitle, oldBody, oldVisibility, oldIdentity string
	if err := a.db.Pool().QueryRow(ctx, `SELECT author_account_id,COALESCE(title,''),body,visibility,COALESCE(identity_kind,'member') FROM posts WHERE id=$1 AND deleted_at IS NULL`, postID).Scan(&owner, &oldTitle, &oldBody, &oldVisibility, &oldIdentity); err != nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "post not found"})
		return
	}
	if owner != m.AccountID {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "you can only edit your own post"})
		return
	}
	if req.Title == oldTitle && req.Body == oldBody && req.Visibility == oldVisibility && req.Identity == oldIdentity {
		writeJSON(w, 200, map[string]any{"ok": true, "unchanged": true})
		return
	}
	authorLabel := m.Name
	roleLabel := firstNonEmpty(m.MembershipLevel, "Member")
	if req.Identity == "organization" {
		authorLabel = "AITS"
		roleLabel = "Organization · posted by " + m.Name + " · " + roleLabel
	}
	if req.Identity == "division" {
		authorLabel = firstNonEmpty(m.Division, "Division")
		roleLabel = "Division · posted by " + m.Name + " · " + roleLabel
	}
	var division any = nil
	if req.Visibility == "division" || req.Identity == "division" {
		division = m.DivisionID
	}
	moderation := "published"
	if req.Visibility == "public" && !m.IsAdmin {
		moderation = "pending"
	}
	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "post could not be updated"})
		return
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO post_history(post_id,editor_account_id,title,body,visibility,identity_kind) VALUES($1,$2,$3,$4,$5,$6)`, postID, m.AccountID, oldTitle, oldBody, oldVisibility, oldIdentity)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE posts SET title=$1,body=$2,visibility=$3,identity_kind=$4,author_label=$5,role_label=$6,division_id=$7::bigint,moderation_status=$8::varchar,published_at=CASE WHEN $8::varchar='published' THEN COALESCE(published_at,NOW()) ELSE NULL END,edited_at=NOW(),updated_at=NOW() WHERE id=$9`, req.Title, req.Body, req.Visibility, req.Identity, authorLabel, roleLabel, division, moderation, postID)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "post could not be updated"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "edited": true, "status": moderation})
}

func (a *appServer) deleteMemberPost(w http.ResponseWriter, r *http.Request, postID int64) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tag, err := a.db.Pool().Exec(ctx, `UPDATE posts SET deleted_at=NOW(),updated_at=NOW() WHERE id=$1 AND author_account_id=$2 AND deleted_at IS NULL`, postID, m.AccountID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "post could not be deleted"})
		return
	}
	if tag.RowsAffected() == 0 {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "post not found or not owned by you"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *appServer) memberPostHistory(w http.ResponseWriter, r *http.Request, postID int64) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var owner int64
	if err := a.db.Pool().QueryRow(ctx, `SELECT author_account_id FROM posts WHERE id=$1 AND deleted_at IS NULL`, postID).Scan(&owner); err != nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "post not found"})
		return
	}
	if owner != m.AccountID {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "post history is private to the author"})
		return
	}
	rows, err := a.db.Pool().Query(ctx, `SELECT id,title,body,visibility,identity_kind,created_at FROM post_history WHERE post_id=$1 AND created_at>NOW()-INTERVAL '1 month' ORDER BY id DESC LIMIT 50`, postID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "history unavailable"})
		return
	}
	defer rows.Close()
	history := []map[string]any{}
	for rows.Next() {
		var id int64
		var title, body, visibility, identity string
		var created time.Time
		if rows.Scan(&id, &title, &body, &visibility, &identity, &created) == nil {
			history = append(history, map[string]any{"id": id, "title": title, "body": body, "visibility": visibility, "identity": identity, "time": formatPublicDateTime(created)})
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "history": history, "retention": "1 month"})
}

func (a *appServer) createMemberPost(w http.ResponseWriter, r *http.Request) {
	m, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	var req struct {
		Title       string           `json:"title"`
		Body        string           `json:"body"`
		Visibility  string           `json:"visibility"`
		Media       []map[string]any `json:"media"`
		ClientNonce string           `json:"client_nonce"`
		Identity    string           `json:"identity"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req) != nil {
		writeJSON(w, 400, map[string]any{"ok": false})
		return
	}
	req.Body = strings.TrimSpace(req.Body)
	req.Title = strings.TrimSpace(req.Title)
	req.Visibility = strings.ToLower(strings.TrimSpace(req.Visibility))
	req.ClientNonce = strings.TrimSpace(req.ClientNonce)
	if len(req.ClientNonce) > 96 {
		req.ClientNonce = req.ClientNonce[:96]
	}
	if (req.Body == "" && len(req.Media) == 0) || len(req.Body) > 10000 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "post text or media is required"})
		return
	}
	if req.Visibility != "public" && req.Visibility != "organization" && req.Visibility != "division" {
		req.Visibility = "organization"
	}
	if len(req.Media) > 12 {
		req.Media = req.Media[:12]
	}
	req.Identity = strings.ToLower(strings.TrimSpace(req.Identity))
	if req.Identity == "" {
		req.Identity = "member"
	}
	canBrand := strings.EqualFold(strings.TrimSpace(m.MembershipLevel), "Officer") || !strings.EqualFold(strings.TrimSpace(m.Role), "member")
	if (req.Identity == "organization" || req.Identity == "division") && !canBrand {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "organization and division identities are limited to officers and elevated access accounts"})
		return
	}
	if req.Identity != "member" && req.Identity != "organization" && req.Identity != "division" {
		req.Identity = "member"
	}
	if req.Identity == "division" && m.DivisionID == 0 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "no division is assigned to this member"})
		return
	}
	authorLabel := m.Name
	identityBits := make([]string, 0, 2)
	if strings.TrimSpace(m.MembershipLevel) != "" {
		identityBits = append(identityBits, m.MembershipLevel)
	}
	if strings.TrimSpace(m.RoleName) != "" && !strings.EqualFold(strings.TrimSpace(m.RoleName), "Member") && !strings.EqualFold(strings.TrimSpace(m.RoleName), strings.TrimSpace(m.MembershipLevel)) {
		identityBits = append(identityBits, m.RoleName)
	}
	roleLabel := strings.Join(identityBits, " · ")
	if roleLabel == "" {
		roleLabel = "Member"
	}
	if req.Identity == "organization" {
		authorLabel = "AITS"
		roleLabel = "Organization · posted by " + m.Name + " · " + roleLabel
	}
	if req.Identity == "division" {
		authorLabel = firstNonEmpty(m.Division, "Division")
		roleLabel = "Division · posted by " + m.Name + " · " + roleLabel
	}
	mediaJSON, _ := json.Marshal(req.Media)
	var division any = nil
	if req.Visibility == "division" || req.Identity == "division" {
		if m.DivisionID == 0 {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "no division is assigned to this member"})
			return
		}
		division = m.DivisionID
	}
	status := "published"
	if req.Visibility == "public" && !m.IsAdmin {
		status = "pending"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var id int64
	var err error
	created := true
	if req.ClientNonce != "" {
		var tag pgconn.CommandTag
		tag, err = a.db.Pool().Exec(ctx, `INSERT INTO posts(author_account_id,title,body,visibility,moderation_status,published_at,author_label,role_label,division_id,media,client_nonce,identity_kind) VALUES($1,$2,$3,$4,$5::varchar,CASE WHEN $5::varchar='published' THEN NOW() ELSE NULL END,$6,$7,$8::bigint,$9::jsonb,$10,$11) ON CONFLICT (author_account_id,client_nonce) WHERE client_nonce IS NOT NULL DO NOTHING`, m.AccountID, req.Title, req.Body, req.Visibility, status, authorLabel, roleLabel, division, string(mediaJSON), req.ClientNonce, req.Identity)
		if isNonceSchemaCompatibilityError(err) {
			// An older database may briefly be missing the nonce migration. Keep
			// posting available; the normal schema startup restores idempotency.
			log.Printf("[POST] nonce schema is not ready; retrying compatibility insert account=%d: %v", m.AccountID, err)
			err = a.db.Pool().QueryRow(ctx, `INSERT INTO posts(author_account_id,title,body,visibility,moderation_status,published_at,author_label,role_label,division_id,media,identity_kind) VALUES($1,$2,$3,$4,$5::varchar,CASE WHEN $5::varchar='published' THEN NOW() ELSE NULL END,$6,$7,$8::bigint,$9::jsonb,$10) RETURNING id`, m.AccountID, req.Title, req.Body, req.Visibility, status, authorLabel, roleLabel, division, string(mediaJSON), req.Identity).Scan(&id)
		} else if err == nil {
			created = tag.RowsAffected() > 0
			err = a.db.Pool().QueryRow(ctx, `SELECT id,moderation_status FROM posts WHERE author_account_id=$1 AND client_nonce=$2 ORDER BY id DESC LIMIT 1`, m.AccountID, req.ClientNonce).Scan(&id, &status)
			if err == nil && !created {
				// The same client action was already accepted. Return the original
				// post before media usage, analytics, or notifications run again.
				writeJSON(w, 200, map[string]any{"ok": true, "id": id, "status": status, "deduplicated": true})
				return
			}
		}
	} else {
		err = a.db.Pool().QueryRow(ctx, `INSERT INTO posts(author_account_id,title,body,visibility,moderation_status,published_at,author_label,role_label,division_id,media,identity_kind) VALUES($1,$2,$3,$4,$5::varchar,CASE WHEN $5::varchar='published' THEN NOW() ELSE NULL END,$6,$7,$8::bigint,$9::jsonb,$10) RETURNING id`, m.AccountID, req.Title, req.Body, req.Visibility, status, authorLabel, roleLabel, division, string(mediaJSON), req.Identity).Scan(&id)
	}
	if err != nil {
		log.Printf("[POST] create member post failed account=%d: %v", m.AccountID, err)
		writeJSON(w, 500, map[string]any{"ok": false, "error": "could not create post"})
		return
	}
	for _, media := range req.Media {
		urlValue, _ := media["url"].(string)
		if strings.TrimSpace(urlValue) == "" {
			urlValue, _ = media["src"].(string)
		}
		urlValue = strings.TrimSpace(urlValue)
		if urlValue == "" {
			continue
		}
		var mediaID int64
		if a.db.Pool().QueryRow(ctx, `SELECT id FROM media_library WHERE url=$1 ORDER BY id DESC LIMIT 1`, urlValue).Scan(&mediaID) == nil {
			_, _ = a.db.Pool().Exec(ctx, `UPDATE media_library SET visibility=$1,division_id=CASE WHEN $1='division' THEN $2::bigint ELSE NULL END,updated_at=NOW() WHERE id=$3`, req.Visibility, m.DivisionID, mediaID)
			_, _ = a.db.Pool().Exec(ctx, `INSERT INTO media_library_usage(media_id,account_id,location_label,action,reason) SELECT $1,$2,$3,'used','Attached to member post' WHERE NOT EXISTS (SELECT 1 FROM media_library_usage WHERE media_id=$1 AND account_id=$2 AND location_label=$3 AND action='used')`, mediaID, m.AccountID, fmt.Sprintf("Member post #%d", id))
		}
	}
	a.recordPostAnalytics(ctx, postActor{AccountID: m.AccountID, Member: &m}, "post_create", id)
	if status == "published" {
		noteTitle := authorLabel + " posted an update"
		noteBody := firstNonEmpty(req.Title, req.Body)
		if len(noteBody) > 180 {
			noteBody = noteBody[:180] + "…"
		}
		rows, notifyErr := a.db.Pool().Query(ctx, `INSERT INTO notifications(account_id,notification_type,title,body,action_url,target_post_id) SELECT mp.account_id,'post',$1,$2,$3,$4 FROM member_profiles mp JOIN accounts ac ON ac.id=mp.account_id WHERE mp.account_id<>$5 AND ac.status='active' AND ($6::varchar<>'division' OR mp.primary_division_id=$7) RETURNING account_id`, noteTitle, noteBody, fmt.Sprintf("/member/posts/%d", id), id, m.AccountID, req.Visibility, m.DivisionID)
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
	if status == "pending" {
		_, _ = a.db.Pool().Exec(ctx, `INSERT INTO notifications(account_id,notification_type,title,body) VALUES($1,'post','Public post submitted','Your public post is waiting for administrator approval.')`, m.AccountID)
		a.notifyMemberNotification(m.AccountID)
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": id, "status": status})
}
