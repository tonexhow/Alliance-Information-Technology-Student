package app

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"aits/internals/uploads"
)

func uploadLimit(header *multipart.FileHeader) (string, int64) {
	ct := strings.ToLower(header.Header.Get("Content-Type"))
	switch {
	case strings.HasPrefix(ct, "image/"):
		return "images", 5 << 20
	case strings.HasPrefix(ct, "audio/"):
		return "audios", 20 << 20
	case strings.HasPrefix(ct, "video/"):
		return "videos", 40 << 20
	default:
		return "files", 10 << 20
	}
}

func (a *appServer) supportUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	_, visitorOK := a.currentOAuthSession(r)
	member, memberOK := a.currentMember(r)
	_ = member
	if !visitorOK && !memberOK {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "sign in before sending an attachment"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 42<<20)
	if err := r.ParseMultipartForm(42 << 20); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "attachment is too large"})
		return
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "choose a file"})
		return
	}
	defer f.Close()
	_, limit := uploadLimit(h)
	result, err := uploads.SavePrivate(filepath.Clean(".."), f, h, "support", limit)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "attachment": result})
}

func (a *appServer) privateMedia(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/api/private-media/")
	parts := strings.Split(raw, "/")
	if len(parts) != 2 || (parts[0] != "support" && parts[0] != "member") || strings.Contains(parts[1], "..") || strings.ContainsAny(parts[1], `\\/`) {
		http.NotFound(w, r)
		return
	}
	mediaURL := "/api/private-media/" + parts[0] + "/" + parts[1]
	needle, _ := json.Marshal([]map[string]string{{"url": mediaURL}})

	authorized := false
	if visitor, ok := a.currentOAuthSession(r); ok && visitor.AccountID > 0 {
		_ = a.db.Pool().QueryRow(r.Context(), `SELECT EXISTS(
			SELECT 1 FROM support_messages sm
			JOIN support_threads st ON st.id=sm.thread_id
			WHERE st.visitor_account_id=$1 AND sm.attachments @> $2::jsonb
		)`, visitor.AccountID, string(needle)).Scan(&authorized)
	}
	if parts[0] == "member" {
		// Public-facing avatars are allowed only when that member is deliberately
		// exposed on the public AITS site (published administration or a visible
		// comment on a published public post). Other member avatars remain private.
		_ = a.db.Pool().QueryRow(r.Context(), `SELECT EXISTS(
			SELECT 1
			FROM administration_members am
			JOIN accounts ac ON ac.id=am.account_id
			WHERE am.is_published=TRUE
			  AND (am.starts_on IS NULL OR am.starts_on<=CURRENT_DATE)
			  AND (am.ends_on IS NULL OR am.ends_on>=CURRENT_DATE)
			  AND ac.avatar_url=$1
			UNION ALL
			SELECT 1
			FROM post_comments pc
			JOIN posts p ON p.id=pc.post_id
			JOIN accounts ac ON ac.id=pc.author_account_id
			WHERE p.moderation_status='published'
			  AND p.visibility='public'
			  AND pc.status='visible'
			  AND ac.avatar_url=$1
		)`, mediaURL).Scan(&authorized)

		// Media attached to an approved public post is intentionally public.
		// Do not overwrite a previous public-avatar authorization with a false
		// post-media lookup result. Everything else still requires the private
		// authorization checks below.
		if !authorized {
			_ = a.db.Pool().QueryRow(r.Context(), `SELECT EXISTS(
				SELECT 1 FROM posts
				WHERE moderation_status='published' AND visibility='public' AND media @> $1::jsonb
			)`, string(needle)).Scan(&authorized)
		}

		if !authorized {
			if member, ok := a.currentMember(r); ok {
				_ = a.db.Pool().QueryRow(r.Context(), `SELECT EXISTS(
					SELECT 1 FROM posts p WHERE p.media @> $1::jsonb AND (
						(p.moderation_status='published' AND (p.visibility='public' OR p.visibility='organization' OR (p.visibility='division' AND p.division_id=$2)))
						OR p.author_account_id=$3 OR $5
					)
					UNION ALL SELECT 1 FROM accounts ac WHERE ac.id=$3 AND ac.avatar_url=$4
				)`, string(needle), member.DivisionID, member.AccountID, mediaURL, member.IsAdmin).Scan(&authorized)
				if !authorized {
					_ = a.db.Pool().QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM accounts WHERE avatar_url=$1 AND account_kind='member')`, mediaURL).Scan(&authorized)
				}
			}
		}
	}

	if !authorized {
		if member, ok := a.currentMember(r); ok {
			_ = a.db.Pool().QueryRow(r.Context(), `SELECT EXISTS(
				SELECT 1 FROM messages msg
				JOIN conversation_members cm ON cm.conversation_id=msg.conversation_id
				WHERE cm.account_id=$1 AND msg.attachments @> $2::jsonb
			)`, member.AccountID, string(needle)).Scan(&authorized)
			if !authorized && member.IsAdmin {
				_ = a.db.Pool().QueryRow(r.Context(), `SELECT EXISTS(
					SELECT 1 FROM support_messages WHERE attachments @> $1::jsonb
				)`, string(needle)).Scan(&authorized)
			}
		}
	}
	if !authorized {
		http.Error(w, "attachment access denied", http.StatusForbidden)
		return
	}

	path := filepath.Join("..", "data", "private", parts[0], parts[1])
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}
	serveManagedFile(w, r, path, "private, max-age=3600")
}

func (a *appServer) memberChatMediaUpload(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentMember(r); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 42<<20)
	if err := r.ParseMultipartForm(42 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "attachment is too large"})
		return
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "choose a file"})
		return
	}
	defer f.Close()
	_, limit := uploadLimit(h)
	result, err := uploads.SavePrivate(filepath.Clean(".."), f, h, "member", limit)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "attachment": result})
}

func (a *appServer) memberMediaUpload(w http.ResponseWriter, r *http.Request) {
	member, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	_ = member
	r.Body = http.MaxBytesReader(w, r.Body, 42<<20)
	if err := r.ParseMultipartForm(42 << 20); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "upload is too large"})
		return
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "choose an image or video"})
		return
	}
	defer f.Close()
	kind, limit := uploadLimit(h)
	if kind != "images" && kind != "videos" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "member posts currently accept images and videos"})
		return
	}
	result, err := uploads.SavePrivate(filepath.Clean(".."), f, h, "member", limit)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	var libraryID int64
	var libraryURL string
	// Member post uploads are content-addressed as well as URL-addressed. This
	// prevents a rapid double submit/retry from creating duplicate Library cards.
	if result.SHA256 != "" && a.db.Pool().QueryRow(r.Context(), `SELECT id,url FROM media_library WHERE content_hash=$1 AND is_active=TRUE ORDER BY id LIMIT 1`, result.SHA256).Scan(&libraryID, &libraryURL) == nil {
		// SavePrivate already wrote this retry to a fresh random file. Reuse the
		// canonical Library asset and remove only the redundant newly-written copy.
		parts := strings.Split(strings.TrimPrefix(result.URL, "/api/private-media/"), "/")
		if len(parts) == 2 {
			_ = os.Remove(filepath.Join("..", "data", "private", parts[0], filepath.Base(parts[1])))
		}
		result.URL = libraryURL
		_, _ = a.db.Pool().Exec(r.Context(), `UPDATE media_library SET is_active=TRUE,visibility='organization',uploaded_by=COALESCE(uploaded_by,$1),updated_at=NOW() WHERE id=$2`, member.AccountID, libraryID)
	} else if a.db.Pool().QueryRow(r.Context(), `SELECT id,url FROM media_library WHERE url=$1 ORDER BY id DESC LIMIT 1`, result.URL).Scan(&libraryID, &libraryURL) != nil {
		_ = a.db.Pool().QueryRow(r.Context(), `INSERT INTO media_library(label,media_type,url,visibility,uploaded_by,is_active,extension,location_label,reason,file_size,source_type,content_hash) VALUES($1,$2,$3,'organization',$4,TRUE,$5,'Member posts','Uploaded from the member post composer',$6,'member-upload',NULLIF($7,'')) RETURNING id`, result.Name, result.Type, result.URL, member.AccountID, strings.TrimPrefix(strings.ToLower(filepath.Ext(result.Name)), "."), result.Size, result.SHA256).Scan(&libraryID)
	} else {
		_, _ = a.db.Pool().Exec(r.Context(), `UPDATE media_library SET is_active=TRUE,visibility='organization',uploaded_by=COALESCE(uploaded_by,$1),content_hash=COALESCE(NULLIF(content_hash,''),NULLIF($3,'')),updated_at=NOW() WHERE id=$2`, member.AccountID, libraryID, result.SHA256)
	}
	if libraryID > 0 {
		_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO media_library_usage(media_id,account_id,location_label,action,reason) SELECT $1,$2,'Member · Post composer','uploaded','Attachment uploaded for a member post.' WHERE NOT EXISTS (SELECT 1 FROM media_library_usage WHERE media_id=$1 AND account_id=$2 AND location_label='Member · Post composer' AND action='uploaded')`, libraryID, member.AccountID)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "media": map[string]any{"src": result.URL, "url": result.URL, "name": result.Name, "type": result.Type, "size": result.Size, "library_id": libraryID}})
}

func (a *appServer) memberAvatarUpload(w http.ResponseWriter, r *http.Request) {
	member, ok := a.currentMember(r)
	if !ok {
		writeJSON(w, 401, map[string]any{"ok": false, "error": "member login required"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
	if err := r.ParseMultipartForm(6 << 20); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "profile image is too large"})
		return
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "choose an image"})
		return
	}
	defer f.Close()
	if kind, _ := uploadLimit(h); kind != "images" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "profile image must be an image"})
		return
	}
	result, err := uploads.SavePrivate(filepath.Clean(".."), f, h, "member", 5<<20)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if _, err = a.db.Pool().Exec(r.Context(), `UPDATE accounts SET avatar_url=$1,updated_at=NOW() WHERE id=$2`, result.URL, member.AccountID); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "profile image could not be saved"})
		return
	}
	var avatarLibraryID int64
	if a.db.Pool().QueryRow(r.Context(), `SELECT id FROM media_library WHERE url=$1 ORDER BY id DESC LIMIT 1`, result.URL).Scan(&avatarLibraryID) != nil {
		_ = a.db.Pool().QueryRow(r.Context(), `INSERT INTO media_library(label,media_type,url,visibility,uploaded_by,is_active,extension,location_label,reason,file_size,source_type) VALUES($1,'image',$2,'organization',$3,TRUE,$4,'Member profiles','Member profile image',$5,'member-avatar') RETURNING id`, result.Name, result.URL, member.AccountID, strings.TrimPrefix(strings.ToLower(filepath.Ext(result.Name)), "."), result.Size).Scan(&avatarLibraryID)
	} else {
		_, _ = a.db.Pool().Exec(r.Context(), `UPDATE media_library SET is_active=TRUE,visibility='organization',uploaded_by=COALESCE(uploaded_by,$1),updated_at=NOW() WHERE id=$2`, member.AccountID, avatarLibraryID)
	}
	if avatarLibraryID > 0 {
		_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO media_library_usage(media_id,account_id,location_label,action,reason) VALUES($1,$2,'Member · Profile','profile','Used as a member profile image.')`, avatarLibraryID, member.AccountID)
	}
	// Keep the active in-memory member session synchronized with PostgreSQL so
	// /api/member/me immediately returns the new image instead of the old cached avatar.
	a.mu.Lock()
	for token, session := range a.memberSessions {
		if session.AccountID == member.AccountID {
			session.Avatar = result.URL
			a.memberSessions[token] = session
		}
	}
	a.mu.Unlock()
	writeJSON(w, 200, map[string]any{"ok": true, "url": result.URL})
}

func inlineMediaExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".mp3", ".wav", ".ogg", ".m4a", ".aac", ".mp4", ".webm", ".mov":
		return true
	default:
		return false
	}
}

func serveManagedFile(w http.ResponseWriter, r *http.Request, path string, cacheControl string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", cacheControl)
	if !inlineMediaExtension(filepath.Ext(path)) {
		name := strings.ReplaceAll(filepath.Base(path), `"`, "")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}
	http.ServeFile(w, r, path)
}

func (a *appServer) libraryMedia(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/library/")
	if raw == "" || strings.Contains(raw, "..") || strings.Contains(raw, `\\`) {
		http.NotFound(w, r)
		return
	}
	root := filepath.Clean(filepath.Join("..", "data", "library"))
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(raw)))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	serveManagedFile(w, r, path, "public, max-age=86400")
}

func (a *appServer) adminLibraryUpload(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 42<<20)
	if err := r.ParseMultipartForm(42 << 20); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "upload is too large"})
		return
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "choose a file"})
		return
	}
	defer f.Close()
	folder, limit := uploadLimit(h)
	result, err := uploads.SavePublic(filepath.Clean(".."), f, h, folder, limit)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	removeFreshUpload := func() {
		if path, managed := managedLibraryFilePath(result.URL); managed {
			_ = os.Remove(path)
		}
	}
	a.backfillLibraryContentHashes(r.Context())
	var duplicateID int64
	var duplicateLabel, duplicateURL, duplicateType string
	var duplicateSize int64
	var duplicateActive bool
	duplicateErr := a.db.Pool().QueryRow(r.Context(), `SELECT id,label,url,media_type,COALESCE(file_size,0),is_active FROM media_library WHERE source_type='upload' AND content_hash=$1 ORDER BY is_active DESC,id LIMIT 1`, result.SHA256).Scan(&duplicateID, &duplicateLabel, &duplicateURL, &duplicateType, &duplicateSize, &duplicateActive)
	if duplicateErr == nil && duplicateID > 0 {
		removeFreshUpload()
		duplicate := map[string]any{"id": duplicateID, "label": duplicateLabel, "url": duplicateURL, "type": duplicateType, "size": duplicateSize, "sha256": result.SHA256, "active": duplicateActive}
		if r.FormValue("reuse_duplicate") != "1" {
			writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "duplicate": true, "error": "This exact file already exists in Library.", "existing": duplicate})
			return
		}
		if !duplicateActive {
			_, _ = a.db.Pool().Exec(r.Context(), `UPDATE media_library SET is_active=TRUE,updated_at=NOW() WHERE id=$1`, duplicateID)
		}
		_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO media_library_usage(media_id,account_id,location_label,action,reason) VALUES($1,$2,'Admin · Library','reused',$3)`, duplicateID, actor.AccountID, "Identical upload detected by SHA-256; existing Library asset reused.")
		writeJSON(w, 200, map[string]any{"ok": true, "id": duplicateID, "item": duplicate, "reused": true})
		return
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(result.Name)), ".")
	reason := strings.TrimSpace(r.FormValue("reason"))
	location := strings.TrimSpace(r.FormValue("location"))
	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		label = result.Name
	}
	var id int64
	err = a.db.Pool().QueryRow(r.Context(), `INSERT INTO media_library(label,media_type,url,visibility,uploaded_by,is_active,extension,location_label,reason,file_size,source_type,content_hash) VALUES($1,$2,$3,'organization',$4,TRUE,$5,$6,$7,$8,'upload',$9) RETURNING id`, label, result.Type, result.URL, actor.AccountID, ext, location, reason, result.Size, result.SHA256).Scan(&id)
	if err != nil {
		removeFreshUpload()
		writeJSON(w, 500, map[string]any{"ok": false, "error": "could not register uploaded file"})
		return
	}
	_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO media_library_usage(media_id,account_id,location_label,action,reason) VALUES($1,$2,'Admin · Library','registered',$3)`, id, actor.AccountID, firstNonEmpty(reason, "Uploaded to the managed Library."))
	writeJSON(w, 200, map[string]any{"ok": true, "id": id, "item": result, "reused": false})
}

func decodeAttachmentJSON(raw string) []supportAttachment {
	var items []supportAttachment
	_ = json.Unmarshal([]byte(raw), &items)
	return items
}
