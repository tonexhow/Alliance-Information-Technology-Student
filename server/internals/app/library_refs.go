package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type libraryReference struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (a *appServer) backfillLibraryContentHashes(ctx context.Context) {
	rows, err := a.db.Pool().Query(ctx, `SELECT id,url FROM media_library WHERE source_type='upload' AND COALESCE(content_hash,'')='' ORDER BY id`)
	if err != nil {
		return
	}
	type item struct {
		id  int64
		url string
	}
	items := make([]item, 0)
	for rows.Next() {
		var x item
		if rows.Scan(&x.id, &x.url) == nil {
			items = append(items, x)
		}
	}
	rows.Close()
	for _, x := range items {
		path, managed := managedLibraryFilePath(x.url)
		if !managed {
			continue
		}
		hash, err := fileSHA256(path)
		if err != nil || hash == "" {
			continue
		}
		_, _ = a.db.Pool().Exec(ctx, `UPDATE media_library SET content_hash=$1 WHERE id=$2 AND COALESCE(content_hash,'')=''`, hash, x.id)
	}
}

func normalizedLibraryURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return "/" + strings.TrimLeft(value, "/")
}

func (a *appServer) libraryReferences(ctx context.Context, mediaID int64, rawURL string) []libraryReference {
	url := normalizedLibraryURL(rawURL)
	bare := strings.TrimLeft(url, "/")
	refs := make([]libraryReference, 0)
	// Static shell assets are referenced by application code rather than a database row.
	// Keep those Library records protected so "Clean up unused" cannot remove their managed identity entry.
	switch url {
	case "/library/images/branding/aits.png":
		refs = append(refs, libraryReference{Type: "system_ui", ID: "aits-brand", Label: "AITS identity · public/member/admin UI"})
	case "/library/images/branding/bcc.png":
		refs = append(refs, libraryReference{Type: "system_ui", ID: "bcc-brand", Label: "BCC institutional identity · About"})
	case "/library/images/branding/cite.png":
		refs = append(refs, libraryReference{Type: "system_ui", ID: "cite-brand", Label: "CITE institutional identity · About"})
	case "/icons/report-bug.svg":
		refs = append(refs, libraryReference{Type: "system_ui", ID: "bug-report", Label: "Bug report UI"})
	}

	rows, err := a.db.Pool().Query(ctx, `SELECT id::text,name FROM divisions WHERE logo_url=$1 OR logo_url=$2 ORDER BY id`, url, rawURL)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, label string
			if rows.Scan(&id, &label) == nil {
				refs = append(refs, libraryReference{Type: "division_logo", ID: id, Label: label})
			}
		}
	}
	rows, err = a.db.Pool().Query(ctx, `SELECT id::text,display_name FROM accounts WHERE avatar_url=$1 OR avatar_url=$2 ORDER BY id LIMIT 100`, url, rawURL)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, label string
			if rows.Scan(&id, &label) == nil {
				refs = append(refs, libraryReference{Type: "profile_avatar", ID: id, Label: label})
			}
		}
	}
	rows, err = a.db.Pool().Query(ctx, `SELECT DISTINCT p.id::text,COALESCE(NULLIF(p.title,''),'Untitled post') FROM posts p, LATERAL jsonb_array_elements(COALESCE(p.media,'[]'::jsonb)) media WHERE COALESCE(media->>'src','') IN ($1,$2,$3) OR ('/'||ltrim(COALESCE(media->>'src',''),'/'))=$1 ORDER BY p.id DESC`, url, bare, rawURL)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, label string
			if rows.Scan(&id, &label) == nil {
				refs = append(refs, libraryReference{Type: "post", ID: id, Label: label})
			}
		}
	}
	rows, err = a.db.Pool().Query(ctx, `SELECT DISTINCT q.id::text,COALESCE(NULLIF(q.question_text,''),'Assessment question') FROM assessment_questions q WHERE q.question_content->>'library_id'=$1 OR q.correct_answer_content->>'library_id'=$1 OR EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(q.dummy_answer_contents,'[]'::jsonb)) d WHERE d->>'library_id'=$1) ORDER BY q.id`, strconv.FormatInt(mediaID, 10))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, label string
			if rows.Scan(&id, &label) == nil {
				refs = append(refs, libraryReference{Type: "assessment", ID: id, Label: label})
			}
		}
	}
	return refs
}

func (a *appServer) libraryReferenceCount(ctx context.Context, mediaID int64, rawURL string) int {
	return len(a.libraryReferences(ctx, mediaID, rawURL))
}

func (a *appServer) archiveLibraryItem(ctx context.Context, actorID, id int64) error {
	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var payload, itemLabel, itemURL string
	if err = tx.QueryRow(ctx, `SELECT jsonb_build_object('item',to_jsonb(ml),'usage',COALESCE((SELECT jsonb_agg(to_jsonb(u)) FROM media_library_usage u WHERE u.media_id=ml.id),'[]'::jsonb))::text,ml.label,ml.url FROM media_library ml WHERE ml.id=$1 FOR UPDATE`, id).Scan(&payload, &itemLabel, &itemURL); err != nil {
		return err
	}
	if refs := a.libraryReferences(ctx, id, itemURL); len(refs) > 0 {
		b, _ := json.Marshal(refs)
		return fmt.Errorf("library item is linked to %d active record(s): %s", len(refs), string(b))
	}
	if _, err = tx.Exec(ctx, `INSERT INTO archive_items(entity_type,entity_id,label,payload,archived_by) VALUES('library',$1,$2,$3::jsonb,$4)`, fmt.Sprint(id), itemLabel, payload, actorID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM media_library WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a *appServer) adminLibraryCleanup(w http.ResponseWriter, r *http.Request) {
	actor, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"ok": false})
		return
	}
	rows, err := a.db.Pool().Query(r.Context(), `SELECT id,url FROM media_library ORDER BY id`)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false})
		return
	}
	type candidate struct {
		id  int64
		url string
	}
	items := []candidate{}
	for rows.Next() {
		var c candidate
		if rows.Scan(&c.id, &c.url) == nil {
			items = append(items, c)
		}
	}
	rows.Close()
	cleaned := 0
	skipped := 0
	for _, c := range items {
		if a.libraryReferenceCount(r.Context(), c.id, c.url) > 0 {
			skipped++
			continue
		}
		if err := a.archiveLibraryItem(r.Context(), actor.AccountID, c.id); err == nil {
			cleaned++
		} else {
			skipped++
		}
	}
	_, _ = a.db.Pool().Exec(r.Context(), `INSERT INTO audit_logs(actor_account_id,action,entity_type,details) VALUES($1,'library.cleanup_unused','library',jsonb_build_object('archived',$2,'skipped',$3))`, actor.AccountID, cleaned, skipped)
	writeJSON(w, 200, map[string]any{"ok": true, "archived": cleaned, "skipped": skipped})
}

func managedLibraryFilePath(rawURL string) (string, bool) {
	url := normalizedLibraryURL(rawURL)
	if !strings.HasPrefix(url, "/library/") {
		return "", false
	}
	relURL := strings.TrimPrefix(url, "/library/")
	if relURL == "" || strings.Contains(relURL, "..") || strings.Contains(relURL, `\\`) {
		return "", false
	}
	for _, root := range []string{filepath.Clean(filepath.Join("..", "data", "library")), filepath.Clean(filepath.Join("data", "library"))} {
		candidate := filepath.Clean(filepath.Join(root, filepath.FromSlash(relURL)))
		rel, err := filepath.Rel(root, candidate)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		} else if !os.IsNotExist(err) {
			return candidate, true
		}
	}
	return filepath.Clean(filepath.Join("..", "data", "library", filepath.FromSlash(relURL))), true
}

func removeManagedLibraryFileFromArchivePayload(payload string) error {
	var wrapped struct {
		Item struct {
			URL        string `json:"url"`
			SourceType string `json:"source_type"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(payload), &wrapped); err != nil {
		return nil
	}
	if strings.ToLower(strings.TrimSpace(wrapped.Item.SourceType)) != "upload" {
		return nil
	}
	path, managed := managedLibraryFilePath(wrapped.Item.URL)
	if !managed {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (a *appServer) purgeExpiredArchiveItems(ctx context.Context) error {
	rows, err := a.db.Pool().Query(ctx, `SELECT id,entity_type,payload::text FROM archive_items WHERE expires_at<=NOW() AND restored_at IS NULL AND permanently_deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return err
	}
	type expired struct {
		id      int64
		typ     string
		payload string
	}
	items := make([]expired, 0)
	for rows.Next() {
		var item expired
		if rows.Scan(&item.id, &item.typ, &item.payload) == nil {
			items = append(items, item)
		}
	}
	rows.Close()
	for _, item := range items {
		if item.typ == "library" {
			if err := removeManagedLibraryFileFromArchivePayload(item.payload); err != nil {
				return fmt.Errorf("remove expired Library file for archive %d: %w", item.id, err)
			}
		}
		if _, err := a.db.Pool().Exec(ctx, `DELETE FROM archive_items WHERE id=$1`, item.id); err != nil {
			return err
		}
	}
	return nil
}
