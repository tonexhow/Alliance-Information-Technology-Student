package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type oauthStateEntry struct {
	ReturnPath   string
	ClientOrigin string
}

func oauthSecretHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func (a *appServer) oauthClientOrigin(raw string) string {
	raw = strings.TrimSpace(strings.TrimRight(raw, "/"))
	if raw == "" {
		return strings.TrimRight(a.config.ClientURL, "/")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return strings.TrimRight(a.config.ClientURL, "/")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "127.0.0.1" || host == "localhost" {
		if parsed.Scheme == "http" && (parsed.Port() == "3014" || parsed.Port() == "") {
			return parsed.Scheme + "://" + parsed.Host
		}
		return strings.TrimRight(a.config.ClientURL, "/")
	}
	publicURL, err := url.Parse(a.config.PublicURL)
	if err == nil && strings.EqualFold(parsed.Scheme, publicURL.Scheme) && strings.EqualFold(parsed.Host, publicURL.Host) {
		return strings.TrimRight(a.config.PublicURL, "/")
	}
	return strings.TrimRight(a.config.ClientURL, "/")
}

func requestUsesHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

func (a *appServer) storeOAuthState(ctx context.Context, rawState, provider, returnPath, clientOrigin string) error {
	if a.db == nil {
		return nil
	}
	_, _ = a.db.Pool().Exec(ctx, `DELETE FROM oauth_states WHERE expires_at<=NOW()`)
	_, err := a.db.Pool().Exec(ctx, `INSERT INTO oauth_states(state_hash,provider,return_path,client_origin,expires_at) VALUES($1,$2,$3,$4,NOW()+INTERVAL '10 minutes')`, oauthSecretHash(rawState), provider, sanitizeReturnPath(returnPath), a.oauthClientOrigin(clientOrigin))
	return err
}

func (a *appServer) consumeOAuthState(ctx context.Context, r *http.Request, provider string) (oauthStateEntry, bool) {
	rawState := strings.TrimSpace(r.URL.Query().Get("state"))
	if rawState == "" {
		return oauthStateEntry{}, false
	}
	if a.db != nil {
		var entry oauthStateEntry
		err := a.db.Pool().QueryRow(ctx, `DELETE FROM oauth_states WHERE state_hash=$1 AND provider=$2 AND expires_at>NOW() RETURNING return_path,client_origin`, oauthSecretHash(rawState), provider).Scan(&entry.ReturnPath, &entry.ClientOrigin)
		if err == nil {
			entry.ReturnPath = sanitizeReturnPath(entry.ReturnPath)
			entry.ClientOrigin = a.oauthClientOrigin(entry.ClientOrigin)
			return entry, true
		}
	}
	if a.validOAuthCallback(r, provider) {
		return oauthStateEntry{ReturnPath: a.oauthReturnPath(r), ClientOrigin: strings.TrimRight(a.config.ClientURL, "/")}, true
	}
	return oauthStateEntry{}, false
}

func (a *appServer) setOAuthSession(w http.ResponseWriter, r *http.Request, accountID int64, user OAuthUser) error {
	sessionID, err := randomToken(36)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.sessions[sessionID] = sessionEntry{AccountID: accountID, User: user, ExpiresAt: time.Now().Add(24 * time.Hour)}
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: "aits_session", Value: sessionID, Path: "/", HttpOnly: true,
		Secure: requestUsesHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: 86400,
	})
	return nil
}

func (a *appServer) createOAuthHandoff(ctx context.Context, accountID int64, provider, clientOrigin string) (string, error) {
	if a.db == nil || accountID < 1 {
		return "", fmt.Errorf("database-backed OAuth handoff is unavailable")
	}
	token, err := randomToken(40)
	if err != nil {
		return "", err
	}
	_, _ = a.db.Pool().Exec(ctx, `DELETE FROM oauth_handoffs WHERE expires_at<=NOW() OR used_at IS NOT NULL`)
	_, err = a.db.Pool().Exec(ctx, `INSERT INTO oauth_handoffs(token_hash,account_id,provider,client_origin,expires_at) VALUES($1,$2,$3,$4,NOW()+INTERVAL '2 minutes')`, oauthSecretHash(token), accountID, provider, a.oauthClientOrigin(clientOrigin))
	if err != nil {
		return "", err
	}
	return token, nil
}

func (a *appServer) oauthHandoff(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "visitor sign-in handoff is unavailable"})
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req) != nil || strings.TrimSpace(req.Token) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid sign-in handoff"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "sign-in handoff could not be opened"})
		return
	}
	defer tx.Rollback(ctx)
	var accountID int64
	var provider, providerID, email, name, picture, clientOrigin string
	err = tx.QueryRow(ctx, `SELECT h.account_id,h.provider,COALESCE(oi.provider_user_id,''),COALESCE(ac.email,''),ac.display_name,COALESCE(ac.avatar_url,''),h.client_origin
        FROM oauth_handoffs h
        JOIN accounts ac ON ac.id=h.account_id
        LEFT JOIN oauth_identities oi ON oi.account_id=h.account_id AND oi.provider=h.provider
        WHERE h.token_hash=$1 AND h.used_at IS NULL AND h.expires_at>NOW()
        FOR UPDATE OF h`, oauthSecretHash(req.Token)).Scan(&accountID, &provider, &providerID, &email, &name, &picture, &clientOrigin)
	if err != nil {
		writeJSON(w, http.StatusGone, map[string]any{"ok": false, "error": "this sign-in handoff has expired or was already used"})
		return
	}
	expectedOrigin := a.oauthClientOrigin(clientOrigin)
	requestOrigin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
	if requestOrigin != "" && !strings.EqualFold(requestOrigin, expectedOrigin) {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "sign-in handoff origin mismatch"})
		return
	}
	if _, err = tx.Exec(ctx, `UPDATE oauth_handoffs SET used_at=NOW() WHERE token_hash=$1`, oauthSecretHash(req.Token)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "sign-in handoff could not be completed"})
		return
	}
	if err = tx.Commit(ctx); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "sign-in handoff could not be completed"})
		return
	}
	user := OAuthUser{Provider: provider, ProviderID: providerID, Email: email, Name: name, Picture: picture}
	if err := a.setOAuthSession(w, r, accountID, user); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "visitor session could not be created"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": user})
}

func (a *appServer) ensureOAuthAccount(ctx context.Context, user OAuthUser) (int64, error) {
	if a.db == nil {
		return 0, fmt.Errorf("database unavailable")
	}
	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var accountID int64
	err = tx.QueryRow(ctx, `SELECT account_id FROM oauth_identities WHERE provider=$1 AND provider_user_id=$2`, user.Provider, user.ProviderID).Scan(&accountID)
	if err != nil {
		if strings.TrimSpace(user.Email) != "" {
			_ = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE LOWER(email)=LOWER($1) LIMIT 1`, user.Email).Scan(&accountID)
		}
		if accountID == 0 {
			err = tx.QueryRow(ctx, `INSERT INTO accounts(email,display_name,avatar_url,account_kind,status,last_login_at) VALUES($1,$2,$3,'visitor','active',NOW()) RETURNING id`, user.Email, user.Name, user.Picture).Scan(&accountID)
			if err != nil {
				return 0, err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO oauth_identities(account_id,provider,provider_user_id,provider_email) VALUES($1,$2,$3,$4) ON CONFLICT(provider,provider_user_id) DO UPDATE SET account_id=EXCLUDED.account_id,provider_email=EXCLUDED.provider_email,updated_at=NOW()`, accountID, user.Provider, user.ProviderID, user.Email)
		if err != nil {
			return 0, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE accounts SET email=COALESCE(NULLIF($1,''),email),display_name=$2,avatar_url=NULLIF($3,''),last_login_at=NOW(),updated_at=NOW() WHERE id=$4`, user.Email, user.Name, user.Picture, accountID)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return accountID, nil
}

func (a *appServer) currentOAuthSession(r *http.Request) (sessionEntry, bool) {
	cookie, err := r.Cookie("aits_session")
	if err != nil || cookie.Value == "" {
		return sessionEntry{}, false
	}
	a.mu.RLock()
	entry, ok := a.sessions[cookie.Value]
	a.mu.RUnlock()
	if !ok || time.Now().After(entry.ExpiresAt) {
		if ok {
			a.mu.Lock()
			delete(a.sessions, cookie.Value)
			a.mu.Unlock()
		}
		return sessionEntry{}, false
	}
	return entry, true
}
