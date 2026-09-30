package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"aits/internals/database"
	"aits/mailer"

	"github.com/jackc/pgx/v5"
)

type OAuthUser struct {
	Provider   string `json:"provider"`
	ProviderID string `json:"provider_id"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	Picture    string `json:"picture"`
}

type sessionEntry struct {
	AccountID int64
	User      OAuthUser
	ExpiresAt time.Time
}

type appServer struct {
	config         Config
	db             *database.Store
	client         *http.Client
	sessions       map[string]sessionEntry
	memberSessions map[string]memberSessionEntry
	mu             sync.RWMutex
	mailer         mailer.Client
	presence       *presenceHub
	support        *supportHub
	logs           *systemLogHub
	chat           *memberChatHub
	publicSync     *publicSyncHub
}

func Run() {
	config := loadConfig()
	systemLogs := newSystemLogHub(config.PublicURL)
	log.SetOutput(io.MultiWriter(os.Stdout, systemLogs))

	var db *database.Store
	if config.DatabaseReady() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		var err error
		db, err = database.Open(ctx, config.DatabaseURL)
		cancel()
		if err != nil {
			log.Fatalf("PostgreSQL startup failed: %v", err)
		}
		defer db.Close()
		log.Printf("PostgreSQL connected and data/database/SCHEMA.sql is ready")
	} else {
		log.Printf("PostgreSQL is not configured; add DATABASE_URL to root .env or [BOOT] Scripts/global.env")
	}

	app := &appServer{
		config: config, db: db, client: &http.Client{Timeout: 15 * time.Second},
		sessions: make(map[string]sessionEntry), memberSessions: make(map[string]memberSessionEntry),
		mailer: mailer.New(mailer.Config{Email: config.SMTPEmail, Password: config.SMTPPassword, Host: config.SMTPHost, Port: config.SMTPPort}), presence: newPresenceHub(config.PublicURL), support: newSupportHub(config.PublicURL), logs: systemLogs, chat: newMemberChatHub(config.PublicURL), publicSync: newPublicSyncHub(config.PublicURL),
	}
	if db != nil {
		if config.DeveloperBootstrapReady() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			if err := app.ensureBootstrapDeveloper(ctx, config.DeveloperUsername, config.DeveloperName, config.DeveloperPassword); err != nil {
				cancel()
				log.Fatalf("bootstrap developer account failed: %v", err)
			}
			cancel()
		}
		if strings.EqualFold(strings.TrimSpace(os.Getenv("AITS_DEV_TEST_ACCOUNTS")), "1") || strings.EqualFold(strings.TrimSpace(os.Getenv("AITS_DEV_TEST_ACCOUNTS")), "true") {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			err := app.ensureDevelopmentTestAccounts(ctx)
			cancel()
			if err != nil {
				// Development helper accounts must never take the real API down. The
				// error is kept visible in the server/admin console so it can be fixed,
				// while normal AITS accounts and public services remain available.
				log.Printf("[DEV] development test accounts warning: %v", err)
			} else {
				log.Printf("[DEV] Local development test accounts are ready (aits{role} / shared development password)")
			}
		}

		archiveCtx, archiveCancel := context.WithTimeout(context.Background(), 8*time.Second)
		if err := app.purgeExpiredArchiveItems(archiveCtx); err != nil {
			log.Printf("archive housekeeping warning: %v", err)
		}
		archiveCancel()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", onlyMethod(http.MethodGet, app.health))
	mux.HandleFunc("/api/system/capabilities", onlyMethod(http.MethodGet, app.capabilities))
	mux.HandleFunc("/api/auth/session", onlyMethod(http.MethodGet, app.authSession))
	mux.HandleFunc("/api/auth/handoff", onlyMethod(http.MethodPost, app.oauthHandoff))
	mux.HandleFunc("/api/auth/google/start", onlyMethod(http.MethodGet, app.googleStart))
	mux.HandleFunc("/api/auth/google/callback", onlyMethod(http.MethodGet, app.googleCallback))
	mux.HandleFunc("/api/auth/github/start", onlyMethod(http.MethodGet, app.githubStart))
	mux.HandleFunc("/api/auth/github/callback", onlyMethod(http.MethodGet, app.githubCallback))
	mux.HandleFunc("/api/auth/logout", onlyMethod(http.MethodPost, app.logout))
	mux.HandleFunc("/api/application/current", onlyMethod(http.MethodGet, app.applicationCurrent))
	mux.HandleFunc("/api/application/questions", onlyMethod(http.MethodGet, app.applicationQuestions))
	mux.HandleFunc("/api/application/finish", onlyMethod(http.MethodPost, app.applicationFinish))
	mux.HandleFunc("/api/application/cancel", onlyMethod(http.MethodPost, app.applicationCancel))
	mux.HandleFunc("/api/application/activation", app.applicationActivation)
	mux.HandleFunc("/api/content/public", onlyMethod(http.MethodGet, app.publicContent))
	mux.HandleFunc("/api/analytics/event", onlyMethod(http.MethodPost, app.analyticsEvent))
	mux.HandleFunc("/api/bug-reports", onlyMethod(http.MethodPost, app.bugReport))
	mux.HandleFunc("/api/posts/", app.withPublicRefresh("posts", app.postRouter))
	mux.HandleFunc("/api/social/profile", onlyMethod(http.MethodGet, app.socialProfileCard))
	mux.HandleFunc("/api/social/mentions", onlyMethod(http.MethodGet, app.socialMentionSearch))
	mux.HandleFunc("/api/member/login", onlyMethod(http.MethodPost, app.memberLogin))
	mux.HandleFunc("/api/member/logout", onlyMethod(http.MethodPost, app.memberLogout))
	mux.HandleFunc("/api/member/session", onlyMethod(http.MethodGet, app.memberSession))
	mux.HandleFunc("/api/member/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			app.memberMe(w, r)
			return
		}
		if r.Method == http.MethodPatch {
			app.updateMemberMe(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/api/member/feed", onlyMethod(http.MethodGet, app.memberFeedV2))
	mux.HandleFunc("/api/member/posts", app.withPublicRefresh("posts", onlyMethod(http.MethodPost, app.createMemberPost)))
	mux.HandleFunc("/api/member/media", onlyMethod(http.MethodPost, app.memberMediaUpload))
	mux.HandleFunc("/api/member/chat-media", onlyMethod(http.MethodPost, app.memberChatMediaUpload))
	mux.HandleFunc("/api/member/avatar", onlyMethod(http.MethodPost, app.memberAvatarUpload))
	mux.HandleFunc("/api/member/directory", onlyMethod(http.MethodGet, app.memberDirectory))
	mux.HandleFunc("/api/member/divisions", onlyMethod(http.MethodGet, app.memberDivisions))
	mux.HandleFunc("/api/member/division-request", onlyMethod(http.MethodPost, app.memberDivisionRequest))
	mux.HandleFunc("/api/member/membership-request", onlyMethod(http.MethodPost, app.memberMembershipRequest))
	mux.HandleFunc("/api/member/security/email/", onlyMethod(http.MethodPost, app.memberEmailSecurity))
	mux.HandleFunc("/api/member/security/password/request", onlyMethod(http.MethodPost, app.requestMemberPasswordChange))
	mux.HandleFunc("/api/member/security/password/confirm", onlyMethod(http.MethodPost, app.confirmMemberPasswordChange))
	mux.HandleFunc("/api/member/recovery/request", onlyMethod(http.MethodPost, app.requestPasswordRecovery))
	mux.HandleFunc("/api/member/recovery/confirm", onlyMethod(http.MethodPost, app.confirmPasswordRecovery))
	mux.HandleFunc("/api/member/admin-reset", app.memberAdminPasswordReset)
	mux.HandleFunc("/api/member/notifications", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			app.memberNotifications(w, r)
			return
		}
		if r.Method == http.MethodPost {
			app.markNotificationsRead(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/api/member/notifications/clear", onlyMethod(http.MethodDelete, app.clearMemberNotifications))
	mux.HandleFunc("/api/member/chats", app.memberChats)
	mux.HandleFunc("/api/member/chats/", app.memberChatItem)
	mux.HandleFunc("/ws/member/chat", app.memberChatSocket)
	mux.HandleFunc("/ws/member/inbox", app.memberChatInboxSocket)
	mux.HandleFunc("/api/admin/overview", onlyMethod(http.MethodGet, app.adminOverview))
	mux.HandleFunc("/api/admin/divisions", app.withPublicRefresh("divisions", onlyMethod(http.MethodGet, app.adminDivisions)))
	mux.HandleFunc("/api/admin/divisions/create", app.withPublicRefresh("divisions", app.adminDivisionCreate))
	mux.HandleFunc("/api/admin/divisions/", app.withPublicRefresh("divisions", app.adminDivisionItem))
	mux.HandleFunc("/api/admin/announcements", app.withPublicRefresh("announcements", app.adminAnnouncements))
	mux.HandleFunc("/api/admin/announcements/", app.withPublicRefresh("announcements", app.adminAnnouncementItem))
	mux.HandleFunc("/api/admin/archive", app.adminArchive)
	mux.HandleFunc("/api/admin/archive/", app.withPublicRefresh("archive", app.adminArchiveItem))
	mux.HandleFunc("/api/admin/member-moderation/", app.withPublicRefresh("members", app.adminMemberModeration))
	mux.HandleFunc("/api/admin/library-usage/", onlyMethod(http.MethodGet, app.adminLibraryUsage))
	mux.HandleFunc("/api/admin/members", app.withPublicRefresh("members", app.adminMembers))
	mux.HandleFunc("/api/admin/members/", app.withPublicRefresh("members", app.adminMemberItem))
	mux.HandleFunc("/api/admin/roles", onlyMethod(http.MethodGet, app.adminRoles))
	mux.HandleFunc("/api/admin/member-requests", onlyMethod(http.MethodGet, app.adminMemberRequests))
	mux.HandleFunc("/api/admin/member-requests/", app.withPublicRefresh("members", app.adminMemberRequestAction))
	mux.HandleFunc("/api/admin/applications", onlyMethod(http.MethodGet, app.adminApplications))
	mux.HandleFunc("/api/admin/applications/", app.adminApplicationItem)
	mux.HandleFunc("/api/admin/assessments", onlyMethod(http.MethodGet, app.adminAssessments))
	mux.HandleFunc("/api/admin/library", app.adminLibrary)
	mux.HandleFunc("/api/admin/library-cleanup", onlyMethod(http.MethodPost, app.adminLibraryCleanup))
	mux.HandleFunc("/api/admin/bug-reports", app.adminBugReports)
	mux.HandleFunc("/api/admin/library/", app.adminLibraryItem)
	mux.HandleFunc("/api/support/upload", onlyMethod(http.MethodPost, app.supportUpload))
	mux.HandleFunc("/api/admin/support/", app.adminSupportThread)
	mux.HandleFunc("/api/private-media/", onlyMethod(http.MethodGet, app.privateMedia))
	mux.HandleFunc("/api/admin/library/upload", onlyMethod(http.MethodPost, app.adminLibraryUpload))
	mux.HandleFunc("/library/", onlyMethod(http.MethodGet, app.libraryMedia))
	mux.HandleFunc("/api/admin/developer/database", app.developerDatabase)
	mux.HandleFunc("/api/admin/developer/system", onlyMethod(http.MethodGet, app.developerSystem))
	mux.HandleFunc("/api/admin/posts", app.withPublicRefresh("posts", app.adminPosts))
	mux.HandleFunc("/api/admin/posts/", app.withPublicRefresh("posts", app.adminPostAction))
	mux.HandleFunc("/api/admin/administration", app.withPublicRefresh("administration", app.adminAdministration))
	mux.HandleFunc("/api/admin/administration/", app.withPublicRefresh("administration", app.adminAdministrationItem))
	mux.HandleFunc("/ws/members", app.memberPresence)
	mux.HandleFunc("/ws/support", app.supportSocket)
	mux.HandleFunc("/ws/public", app.publicContentSocket)
	mux.HandleFunc("/ws/admin/system", app.developerSystemSocket)

	server := &http.Server{
		Addr:              "127.0.0.1:" + config.Port,
		Handler:           securityHeaders(app.requestActivityLogger(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("AITS server starting: %s", config.String())
	log.Printf("Google OAuth redirect URI: %s", config.GoogleRedirectURL)
	log.Printf("GitHub OAuth redirect URI: %s", config.GitHubRedirectURL)
	if !config.GoogleOAuthReady() {
		log.Printf("Google OAuth is not configured; add GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET to root .env or [BOOT] Scripts/global.env")
	}
	if !config.GitHubOAuthReady() {
		log.Printf("GitHub OAuth is not configured; add GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET to root .env or [BOOT] Scripts/global.env")
	}
	if !config.SMTPReady() {
		log.Printf("SMTP is not configured; add SMTP_EMAIL and SMTP_PASSWORD to root .env or [BOOT] Scripts/global.env")
	}
	log.Fatal(server.ListenAndServe())
}

func (a *appServer) health(w http.ResponseWriter, r *http.Request) {
	databaseReady := false
	if a.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		databaseReady = a.db.Ping(ctx) == nil
		cancel()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"service":      "AITS API",
		"port":         a.config.Port,
		"server_time":  time.Now().Format(time.RFC3339Nano),
		"database":     databaseReady,
		"google_oauth": a.config.GoogleOAuthReady(),
		"github_oauth": a.config.GitHubOAuthReady(),
		"smtp":         a.config.SMTPReady(),
	})
}

func (a *appServer) capabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"database":     a.db != nil,
		"google_oauth": a.config.GoogleOAuthReady(),
		"github_oauth": a.config.GitHubOAuthReady(),
		"smtp":         a.config.SMTPReady(),
	})
}

func (a *appServer) authSession(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "user": user})
}

func (a *appServer) googleStart(w http.ResponseWriter, r *http.Request) {
	if !a.config.GoogleOAuthReady() {
		http.Error(w, "Google sign-in is currently unavailable", http.StatusServiceUnavailable)
		return
	}

	returnPath := sanitizeReturnPath(r.URL.Query().Get("return"))
	clientOrigin := a.oauthClientOrigin(r.URL.Query().Get("origin"))
	a.setOAuthReturn(w, returnPath)
	state, ok := a.setOAuthState(w, "google")
	if !ok {
		http.Error(w, "Could not start sign-in", http.StatusInternalServerError)
		return
	}
	if err := a.storeOAuthState(r.Context(), state, "google", returnPath, clientOrigin); err != nil {
		log.Printf("Google OAuth state persistence failed: %v", err)
		http.Error(w, "Could not start sign-in", http.StatusInternalServerError)
		return
	}

	values := url.Values{}
	values.Set("client_id", a.config.GoogleClientID)
	values.Set("redirect_uri", a.config.GoogleRedirectURL)
	values.Set("response_type", "code")
	values.Set("scope", "openid email profile")
	values.Set("state", state)
	values.Set("include_granted_scopes", "true")
	values.Set("prompt", "select_account")
	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+values.Encode(), http.StatusFound)
}

func (a *appServer) googleCallback(w http.ResponseWriter, r *http.Request) {
	state, ok := a.consumeOAuthState(r.Context(), r, "google")
	if !ok {
		a.oauthFail(w, r, "google")
		return
	}
	if r.URL.Query().Get("error") != "" {
		a.oauthFailState(w, r, "google", state)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		a.oauthFailState(w, r, "google", state)
		return
	}

	token, err := a.exchangeGoogleCode(r.Context(), code)
	if err != nil {
		log.Printf("Google token exchange failed: %v", err)
		a.oauthFailState(w, r, "google", state)
		return
	}
	user, err := a.fetchGoogleUser(r.Context(), token)
	if err != nil {
		log.Printf("Google userinfo failed: %v", err)
		a.oauthFailState(w, r, "google", state)
		return
	}
	if user.ProviderID == "" || user.Email == "" {
		a.oauthFailState(w, r, "google", state)
		return
	}

	a.completeOAuth(w, r, user, "google", state)
}

func (a *appServer) githubStart(w http.ResponseWriter, r *http.Request) {
	if !a.config.GitHubOAuthReady() {
		http.Error(w, "GitHub sign-in is currently unavailable", http.StatusServiceUnavailable)
		return
	}

	returnPath := sanitizeReturnPath(r.URL.Query().Get("return"))
	clientOrigin := a.oauthClientOrigin(r.URL.Query().Get("origin"))
	a.setOAuthReturn(w, returnPath)
	state, ok := a.setOAuthState(w, "github")
	if !ok {
		http.Error(w, "Could not start sign-in", http.StatusInternalServerError)
		return
	}
	if err := a.storeOAuthState(r.Context(), state, "github", returnPath, clientOrigin); err != nil {
		log.Printf("GitHub OAuth state persistence failed: %v", err)
		http.Error(w, "Could not start sign-in", http.StatusInternalServerError)
		return
	}

	values := url.Values{}
	values.Set("client_id", a.config.GitHubClientID)
	values.Set("redirect_uri", a.config.GitHubRedirectURL)
	values.Set("scope", "read:user user:email")
	values.Set("state", state)
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+values.Encode(), http.StatusFound)
}

func (a *appServer) githubCallback(w http.ResponseWriter, r *http.Request) {
	state, ok := a.consumeOAuthState(r.Context(), r, "github")
	if !ok {
		a.oauthFail(w, r, "github")
		return
	}
	if r.URL.Query().Get("error") != "" {
		a.oauthFailState(w, r, "github", state)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		a.oauthFailState(w, r, "github", state)
		return
	}

	token, err := a.exchangeGitHubCode(r.Context(), code)
	if err != nil {
		log.Printf("GitHub token exchange failed: %v", err)
		a.oauthFailState(w, r, "github", state)
		return
	}
	user, err := a.fetchGitHubUser(r.Context(), token)
	if err != nil {
		log.Printf("GitHub userinfo failed: %v", err)
		a.oauthFailState(w, r, "github", state)
		return
	}
	if user.ProviderID == "" || user.Email == "" {
		log.Printf("GitHub OAuth did not return a usable verified email")
		a.oauthFailState(w, r, "github", state)
		return
	}

	a.completeOAuth(w, r, user, "github", state)
}

func (a *appServer) setOAuthState(w http.ResponseWriter, provider string) (string, bool) {
	state, err := randomToken(32)
	if err != nil {
		return "", false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "aits_oauth_state_" + provider,
		Value:    state,
		Path:     "/api/auth/" + provider,
		HttpOnly: true,
		Secure:   strings.HasPrefix(a.config.PublicURL, "https://"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	return state, true
}

func (a *appServer) validOAuthCallback(r *http.Request, provider string) bool {
	stateCookie, err := r.Cookie("aits_oauth_state_" + provider)
	if err != nil || stateCookie.Value == "" || r.URL.Query().Get("state") != stateCookie.Value {
		return false
	}
	return r.URL.Query().Get("error") == ""
}

func (a *appServer) completeOAuth(w http.ResponseWriter, r *http.Request, user OAuthUser, provider string, state oauthStateEntry) {
	accountID := int64(0)
	if a.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		resolved, resolveErr := a.ensureOAuthAccount(ctx, user)
		cancel()
		if resolveErr != nil {
			log.Printf("OAuth account sync failed: %v", resolveErr)
			a.oauthFailState(w, r, provider, state)
			return
		}
		accountID = resolved
	}

	origin := a.oauthClientOrigin(state.ClientOrigin)
	returnPath := sanitizeReturnPath(state.ReturnPath)
	parsedOrigin, _ := url.Parse(origin)
	isLocal := parsedOrigin != nil && (strings.EqualFold(parsedOrigin.Hostname(), "127.0.0.1") || strings.EqualFold(parsedOrigin.Hostname(), "localhost"))
	if isLocal {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		handoff, err := a.createOAuthHandoff(ctx, accountID, provider, origin)
		cancel()
		if err != nil {
			log.Printf("OAuth localhost handoff failed: %v", err)
			a.oauthFailState(w, r, provider, state)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "aits_oauth_state_" + provider, Value: "", Path: "/api/auth/" + provider, MaxAge: -1, HttpOnly: true})
		http.SetCookie(w, &http.Cookie{Name: "aits_oauth_return", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
		target := origin + returnPath + oauthQuery(returnPath, "handoff", provider) + "&handoff=" + url.QueryEscape(handoff)
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	if err := a.setOAuthSession(w, r, accountID, user); err != nil {
		a.oauthFailState(w, r, provider, state)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "aits_oauth_state_" + provider, Value: "", Path: "/api/auth/" + provider, MaxAge: -1, HttpOnly: true})
	http.SetCookie(w, &http.Cookie{Name: "aits_oauth_return", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, origin+returnPath+oauthQuery(returnPath, "success", provider), http.StatusFound)
}

func (a *appServer) oauthFailState(w http.ResponseWriter, r *http.Request, provider string, state oauthStateEntry) {
	origin := a.oauthClientOrigin(state.ClientOrigin)
	returnPath := sanitizeReturnPath(state.ReturnPath)
	http.SetCookie(w, &http.Cookie{Name: "aits_oauth_state_" + provider, Value: "", Path: "/api/auth/" + provider, MaxAge: -1, HttpOnly: true})
	http.SetCookie(w, &http.Cookie{Name: "aits_oauth_return", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, origin+returnPath+oauthQuery(returnPath, "error", provider), http.StatusFound)
}

func (a *appServer) oauthFail(w http.ResponseWriter, r *http.Request, provider string) {
	state := oauthStateEntry{ReturnPath: a.oauthReturnPath(r), ClientOrigin: a.config.ClientURL}
	a.oauthFailState(w, r, provider, state)
}

func sanitizeReturnPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return "/"
	}
	if strings.ContainsAny(value, "\r\n") {
		return "/"
	}
	return value
}

func (a *appServer) setOAuthReturn(w http.ResponseWriter, path string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "aits_oauth_return",
		Value:    sanitizeReturnPath(path),
		Path:     "/",
		HttpOnly: true,
		Secure:   strings.HasPrefix(a.config.PublicURL, "https://"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
}

func (a *appServer) oauthReturnPath(r *http.Request) string {
	cookie, err := r.Cookie("aits_oauth_return")
	if err != nil {
		return "/"
	}
	return sanitizeReturnPath(cookie.Value)
}

func oauthQuery(path, status, provider string) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return separator + "oauth=" + url.QueryEscape(status) + "&provider=" + url.QueryEscape(provider)
}

func applicationYearLevel(raw string) int {
	raw = strings.ToLower(strings.TrimSpace(raw))
	for year := 4; year >= 1; year-- {
		if strings.Contains(raw, fmt.Sprintf("%d", year)) {
			return year
		}
	}
	return 1
}

func applicationAnswerPayload(c assessmentContent) map[string]any {
	text := assessmentContentLabel(c)
	value := assessmentContentKey(c)
	out := map[string]any{"text": text, "value": value, "type": c.Type}
	if c.Type != "text" {
		out["url"] = c.URL
		out["label"] = c.Label
		if c.Type == "image" {
			out["image"] = c.URL
		}
	}
	return out
}

func (a *appServer) applicationQuestions(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "assessment service is unavailable"})
		return
	}
	if _, ok := a.currentOAuthSession(r); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "visitor sign-in required"})
		return
	}
	year := applicationYearLevel(r.URL.Query().Get("year_level"))
	rows, err := a.db.Pool().Query(r.Context(), `
		SELECT q.id,COALESCE(d.code,'general'),COALESCE(q.question_text,q.prompt,''),
		       q.question_content::text,q.correct_answer_content::text,q.dummy_answer_contents::text,q.year_level
		FROM assessment_questions q
		LEFT JOIN divisions d ON d.id=q.division_id
		WHERE q.is_active=TRUE AND q.assessment_kind='qa' AND q.year_level <= $1
		  AND (q.category='general' OR (q.category='division' AND d.id IS NOT NULL AND d.is_active=TRUE))
		ORDER BY q.year_level,q.sort_order,q.id`, year)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "assessment bank could not be loaded"})
		return
	}
	defer rows.Close()
	questions := []map[string]any{}
	for rows.Next() {
		var id int64
		var category, fallback, qraw, craw, draw string
		var level int
		if rows.Scan(&id, &category, &fallback, &qraw, &craw, &draw, &level) != nil {
			continue
		}
		var qc, cc assessmentContent
		var dc []assessmentContent
		_ = json.Unmarshal([]byte(qraw), &qc)
		_ = json.Unmarshal([]byte(craw), &cc)
		_ = json.Unmarshal([]byte(draw), &dc)
		if qc.Type == "" {
			qc = assessmentContent{Type: "text", Text: fallback}
		}
		if cc.Type == "" || len(dc) < 3 {
			continue
		}
		dummies := make([]map[string]any, 0, len(dc))
		for _, d := range dc {
			dummies = append(dummies, applicationAnswerPayload(d))
		}
		questions = append(questions, map[string]any{
			"id": id, "category": category, "question": assessmentContentLabel(qc),
			"questionContent": qc, "correctAnswer": applicationAnswerPayload(cc),
			"dummyAnswers": dummies, "enabled": true, "yearLevel": level,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "year_level": year, "questions": questions})
}

func (a *appServer) applicationCurrent(w http.ResponseWriter, r *http.Request) {
	entry, ok := a.currentOAuthSession(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "visitor sign-in required"})
		return
	}
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "applications are temporarily unavailable"})
		return
	}

	var applicationNumber, status, mode, divisionCode, divisionName, birthDate, gender, department, course, yearLevel, schoolID string
	var overall, responseScore float64
	var submittedAt time.Time
	err := a.db.Pool().QueryRow(r.Context(), `
		SELECT ap.application_number,ap.status,ap.assessment_mode,
		       COALESCE(d.code,''),COALESCE(d.name,''),
		       COALESCE(TO_CHAR(ap.birth_date,'YYYY-MM-DD'),''),
		       COALESCE(ap.gender,''),COALESCE(ap.department,''),COALESCE(ap.course,''),
		       COALESCE(ap.year_level,''),COALESCE(ap.school_id,''),
		       COALESCE(ap.overall_score,0),COALESCE(ap.response_score,0),
		       COALESCE(ap.submitted_at,ap.created_at)
		FROM applications ap
		LEFT JOIN divisions d ON d.id=ap.target_division_id
		WHERE ap.account_id=$1
		ORDER BY COALESCE(ap.submitted_at,ap.created_at) DESC
		LIMIT 1`, entry.AccountID).Scan(
		&applicationNumber, &status, &mode, &divisionCode, &divisionName, &birthDate, &gender,
		&department, &course, &yearLevel, &schoolID, &overall, &responseScore, &submittedAt,
	)
	if err == pgx.ErrNoRows {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "application": nil})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "application status is unavailable"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "application": map[string]any{
		"application_number": applicationNumber,
		"status":             status,
		"assessment_mode":    mode,
		"division_code":      divisionCode,
		"division":           divisionName,
		"birthdate":          birthDate,
		"gender":             gender,
		"department":         department,
		"course":             course,
		"year_level":         yearLevel,
		"school_id":          schoolID,
		"overall":            int(overall + 0.5),
		"confidence":         int(responseScore + 0.5),
		"submitted_at":       submittedAt.Format(time.RFC3339),
	}})
}

func normalizedAssessmentAnswer(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func focusPenaltyPoints(incidents int) int {
	events := incidents / 3
	points := 0
	next := 1
	for i := 0; i < events; i++ {
		points += next
		if points >= 100 {
			return 100
		}
		next *= 2
	}
	return points
}

func (a *appServer) applicationFinish(w http.ResponseWriter, r *http.Request) {
	entry, ok := a.currentOAuthSession(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "visitor sign-in required"})
		return
	}
	if a.db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "applications are temporarily unavailable"})
		return
	}

	var request struct {
		Email          string `json:"email"`
		Name           string `json:"name"`
		Division       string `json:"division"`
		AssessmentMode string `json:"assessment_mode"`
		BirthDate      string `json:"birthdate"`
		Gender         string `json:"gender"`
		Department     string `json:"department"`
		Course         string `json:"course"`
		YearLevel      string `json:"year_level"`
		SchoolID       string `json:"school_id"`
		Overall        int    `json:"overall"`
		Confidence     int    `json:"confidence"`
		FocusIncidents int    `json:"focus_incidents"`
		Results        []struct {
			QuestionID   int64   `json:"questionId"`
			Category     string  `json:"category"`
			Correct      bool    `json:"correct"`
			TimedOut     bool    `json:"timedOut"`
			Selected     *string `json:"selected"`
			SelectedKey  *string `json:"selectedKey"`
			SpeedPercent int     `json:"speedPercent"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 96<<10)).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid application details"})
		return
	}
	request.Email = strings.TrimSpace(request.Email)
	request.Name = strings.TrimSpace(request.Name)
	request.Division = strings.TrimSpace(request.Division)
	if request.Email == "" || request.Name == "" || request.Division == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "complete the required application details"})
		return
	}
	if !strings.EqualFold(entry.User.Email, request.Email) {
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "the application email must match your verified visitor email"})
		return
	}
	if len(request.Results) < 20 || len(request.Results) > 60 || request.FocusIncidents < 0 || request.FocusIncidents > 500 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "assessment result is incomplete or invalid"})
		return
	}

	mode := "specific"
	if strings.EqualFold(strings.TrimSpace(request.AssessmentMode), "discover") || strings.EqualFold(strings.TrimSpace(request.AssessmentMode), "discovery") {
		mode = "discovery"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	var divisionID int64
	var divisionCode string
	divisionKey := strings.ToLower(strings.TrimSpace(request.Division))
	err := a.db.Pool().QueryRow(ctx, `SELECT id,code FROM divisions WHERE is_active=TRUE AND (LOWER(name)=LOWER($1) OR LOWER(code)=LOWER($1)) LIMIT 1`, divisionKey).Scan(&divisionID, &divisionCode)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "selected division is unavailable"})
		return
	}
	divisionCode = strings.ToLower(strings.TrimSpace(divisionCode))
	year := applicationYearLevel(request.YearLevel)

	var birth any
	if strings.TrimSpace(request.BirthDate) != "" {
		if _, err := time.Parse("2006-01-02", strings.TrimSpace(request.BirthDate)); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "birthdate is invalid"})
			return
		}
		birth = strings.TrimSpace(request.BirthDate)
	}

	tx, err := a.db.Pool().Begin(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "application could not be submitted"})
		return
	}
	defer tx.Rollback(ctx)

	type verifiedResult struct {
		QuestionID   int64
		Category     string
		Correct      bool
		TimedOut     bool
		SelectedText string
		SelectedKey  string
		SpeedPercent int
	}
	verified := make([]verifiedResult, 0, len(request.Results))
	seenQuestions := make(map[int64]struct{}, len(request.Results))
	categoryTotal := map[string]int{}
	categoryCorrect := map[string]int{}
	correctAnswers := 0
	speedTotal := 0

	for _, item := range request.Results {
		if item.QuestionID < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "assessment contains an invalid question"})
			return
		}
		if _, exists := seenQuestions[item.QuestionID]; exists {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "assessment contains a repeated question"})
			return
		}
		seenQuestions[item.QuestionID] = struct{}{}

		var active bool
		var qYear int
		var category, qDivisionCode, correctRaw, correctFallback string
		var qDivisionID *int64
		err = tx.QueryRow(ctx, `
			SELECT q.is_active,q.year_level,q.category,COALESCE(d.code,''),d.id,
			       q.correct_answer_content::text,COALESCE(q.correct_answer,'')
			FROM assessment_questions q
			LEFT JOIN divisions d ON d.id=q.division_id
			WHERE q.id=$1 AND q.assessment_kind='qa'`, item.QuestionID).
			Scan(&active, &qYear, &category, &qDivisionCode, &qDivisionID, &correctRaw, &correctFallback)
		if err != nil || !active || qYear > year {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "assessment contains a question that is not eligible for this application"})
			return
		}

		categoryCode := "general"
		if category == "division" {
			categoryCode = strings.ToLower(strings.TrimSpace(qDivisionCode))
			if categoryCode == "" || qDivisionID == nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "assessment division question is invalid"})
				return
			}
			if mode == "specific" && *qDivisionID != divisionID {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "assessment contains questions outside the selected division"})
				return
			}
		}

		var correctContent assessmentContent
		_ = json.Unmarshal([]byte(correctRaw), &correctContent)
		if correctContent.Type == "" {
			correctContent = assessmentContent{Type: "text", Text: correctFallback}
		}
		correctKey := assessmentContentKey(correctContent)
		correctLabel := assessmentContentLabel(correctContent)

		selectedText := ""
		if item.Selected != nil {
			selectedText = strings.TrimSpace(*item.Selected)
		}
		selectedKey := ""
		if item.SelectedKey != nil {
			selectedKey = strings.TrimSpace(*item.SelectedKey)
		}
		isCorrect := false
		if !item.TimedOut && selectedText != "" {
			if selectedKey != "" {
				isCorrect = normalizedAssessmentAnswer(selectedKey) == normalizedAssessmentAnswer(correctKey)
			} else {
				isCorrect = normalizedAssessmentAnswer(selectedText) == normalizedAssessmentAnswer(correctLabel)
			}
		}

		speed := item.SpeedPercent
		if speed < 0 {
			speed = 0
		}
		if speed > 100 {
			speed = 100
		}
		categoryTotal[categoryCode]++
		if isCorrect {
			categoryCorrect[categoryCode]++
			correctAnswers++
		}
		speedTotal += speed
		verified = append(verified, verifiedResult{
			QuestionID: item.QuestionID, Category: categoryCode, Correct: isCorrect, TimedOut: item.TimedOut,
			SelectedText: selectedText, SelectedKey: selectedKey, SpeedPercent: speed,
		})
	}

	if mode == "specific" {
		if categoryTotal[divisionCode] < 15 || categoryTotal["general"] < 5 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "assessment does not contain the required division/general question mix"})
			return
		}
	} else {
		for _, category := range []string{"codehex", "illustrados", "neo-tech", "general"} {
			if categoryTotal[category] < 5 {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "discovery assessment does not contain the required question mix"})
				return
			}
		}
	}

	rawOverall := int(float64(correctAnswers)*100.0/float64(len(verified)) + 0.5)
	penaltyPoints := focusPenaltyPoints(request.FocusIncidents)
	overall := rawOverall - penaltyPoints
	if overall < 0 {
		overall = 0
	}
	// The current AITS passing requirement is 50%; reaching half of that requirement
	// through focus penalties forces the assessment score to 10.
	if penaltyPoints >= 25 {
		overall = 10
	}
	responseScore := int(float64(speedTotal)/float64(len(verified)) + 0.5)
	generalScore := 0
	if categoryTotal["general"] > 0 {
		generalScore = int(float64(categoryCorrect["general"])*100.0/float64(categoryTotal["general"]) + 0.5)
	}
	divisionScore := 0
	if categoryTotal[divisionCode] > 0 {
		divisionScore = int(float64(categoryCorrect[divisionCode])*100.0/float64(categoryTotal[divisionCode]) + 0.5)
	}

	randomPart, _ := randomToken(5)
	randomPart = strings.ToUpper(strings.NewReplacer("-", "", "_", "").Replace(randomPart))
	if len(randomPart) > 6 {
		randomPart = randomPart[:6]
	}
	applicationNumber := fmt.Sprintf("AITS-%d-%s", time.Now().Year(), randomPart)

	var applicationID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO applications (
			application_number,account_id,assessment_mode,target_division_id,birth_date,gender,
			department,course,year_level,school_id,status,submitted_at,overall_score,response_score
		) VALUES ($1,$2,$3,$4,$5::date,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),NULLIF($10,''),'submitted',NOW(),$11,$12)
		RETURNING id`, applicationNumber, entry.AccountID, mode, divisionID, birth,
		strings.TrimSpace(request.Gender), strings.TrimSpace(request.Department), strings.TrimSpace(request.Course),
		strings.TrimSpace(request.YearLevel), strings.TrimSpace(request.SchoolID), overall, responseScore).Scan(&applicationID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "application could not be submitted"})
		return
	}

	var attemptID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO assessment_attempts(
			application_id,assessment_mode,target_division_id,status,total_questions,correct_answers,
			overall_score,general_score,division_score,response_speed_ms,focus_incidents,completed_at
		) VALUES($1,$2,$3,'completed',$4,$5,$6,$7,$8,$9,$10,NOW())
		RETURNING id`,
		applicationID, mode, divisionID, len(verified), correctAnswers, overall, generalScore, divisionScore,
		float64(100-responseScore)*100.0, request.FocusIncidents).Scan(&attemptID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "assessment attempt could not be recorded"})
		return
	}

	for i, item := range verified {
		var selectedChoice any
		if item.SelectedText != "" {
			var choiceID int64
			if tx.QueryRow(ctx, `SELECT id FROM assessment_choices WHERE question_id=$1 AND LOWER(TRIM(answer_text))=LOWER(TRIM($2)) ORDER BY id LIMIT 1`, item.QuestionID, item.SelectedText).Scan(&choiceID) == nil {
				selectedChoice = choiceID
			}
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO assessment_attempt_items(
				attempt_id,question_id,position,selected_choice_id,selected_answer_text,selected_answer_key,
				is_correct,timed_out,response_ms
			) VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,$9)
			ON CONFLICT(attempt_id,position) DO NOTHING`,
			attemptID, item.QuestionID, i+1, selectedChoice, item.SelectedText, item.SelectedKey,
			item.Correct, item.TimedOut, max(0, 100-item.SpeedPercent)*100)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "assessment answers could not be recorded"})
			return
		}
	}

	if _, err = tx.Exec(ctx, `UPDATE accounts SET display_name=$1,updated_at=NOW() WHERE id=$2`, request.Name, entry.AccountID); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "application could not be submitted"})
		return
	}
	if err = tx.Commit(ctx); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "application could not be submitted"})
		return
	}

	mailed := false
	if a.config.SMTPReady() {
		if err := a.mailer.SendApplication(request.Email, request.Name, applicationNumber, a.config.PublicURL); err != nil {
			log.Printf("application confirmation email failed: %v", err)
		} else {
			mailed = true
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "application_number": applicationNumber, "mailed": mailed,
		"overall": overall, "confidence": responseScore, "focus_penalty": penaltyPoints,
	})
}

func (a *appServer) applicationCancel(w http.ResponseWriter, r *http.Request) {
	entry, ok := a.currentOAuthSession(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "visitor sign-in required"})
		return
	}
	var request struct {
		ApplicationNumber string `json:"application_number"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&request) != nil || strings.TrimSpace(request.ApplicationNumber) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "application number is required"})
		return
	}
	result, err := a.db.Pool().Exec(r.Context(), `UPDATE applications SET status='cancelled',updated_at=NOW() WHERE account_id=$1 AND application_number=$2 AND status NOT IN ('accepted','declined','cancelled')`, entry.AccountID, strings.TrimSpace(request.ApplicationNumber))
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "application could not be cancelled"})
		return
	}
	if result.RowsAffected() == 0 {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "this application cannot be cancelled"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Application cancelled."})
}

func (a *appServer) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("aits_session"); err == nil {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "aits_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *appServer) currentUser(r *http.Request) (OAuthUser, bool) {
	entry, ok := a.currentOAuthSession(r)
	if !ok {
		return OAuthUser{}, false
	}
	return entry.User, true
}

func (a *appServer) exchangeGoogleCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", a.config.GoogleClientID)
	form.Set("client_secret", a.config.GoogleClientSecret)
	form.Set("redirect_uri", a.config.GoogleRedirectURL)
	form.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("token endpoint returned %s: %s", resp.Status, string(body))
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.AccessToken == "" {
		return "", fmt.Errorf("missing access token")
	}
	return payload.AccessToken, nil
}

func (a *appServer) fetchGoogleUser(ctx context.Context, accessToken string) (OAuthUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openidconnect.googleapis.com/v1/userinfo", nil)
	if err != nil {
		return OAuthUser{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := a.client.Do(req)
	if err != nil {
		return OAuthUser{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OAuthUser{}, fmt.Errorf("userinfo endpoint returned %s", resp.Status)
	}
	var profile struct {
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return OAuthUser{}, err
	}
	return OAuthUser{Provider: "google", ProviderID: profile.Sub, Email: profile.Email, Name: profile.Name, Picture: profile.Picture}, nil
}

func (a *appServer) exchangeGitHubCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", a.config.GitHubClientID)
	form.Set("client_secret", a.config.GitHubClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", a.config.GitHubRedirectURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("GitHub token endpoint returned %s: %s", resp.Status, string(body))
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.AccessToken == "" {
		return "", fmt.Errorf("missing GitHub access token: %s", payload.Error)
	}
	return payload.AccessToken, nil
}

func (a *appServer) fetchGitHubUser(ctx context.Context, accessToken string) (OAuthUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return OAuthUser{}, err
	}
	setGitHubHeaders(req, accessToken)
	resp, err := a.client.Do(req)
	if err != nil {
		return OAuthUser{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OAuthUser{}, fmt.Errorf("GitHub user endpoint returned %s", resp.Status)
	}
	var profile struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return OAuthUser{}, err
	}

	email := strings.TrimSpace(profile.Email)
	if email == "" {
		verifiedEmail, err := a.fetchGitHubPrimaryEmail(ctx, accessToken)
		if err != nil {
			return OAuthUser{}, err
		}
		email = verifiedEmail
	}
	name := firstNonEmpty(profile.Name, profile.Login)
	return OAuthUser{
		Provider:   "github",
		ProviderID: fmt.Sprintf("%d", profile.ID),
		Email:      email,
		Name:       name,
		Picture:    profile.AvatarURL,
	}, nil
}

func (a *appServer) fetchGitHubPrimaryEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
	if err != nil {
		return "", err
	}
	setGitHubHeaders(req, accessToken)
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub email endpoint returned %s", resp.Status)
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", err
	}
	sort.SliceStable(emails, func(i, j int) bool {
		if emails[i].Primary != emails[j].Primary {
			return emails[i].Primary
		}
		return emails[i].Verified && !emails[j].Verified
	})
	for _, item := range emails {
		if item.Verified && strings.TrimSpace(item.Email) != "" {
			return item.Email, nil
		}
	}
	return "", fmt.Errorf("GitHub account has no verified email available to the app")
}

func setGitHubHeaders(req *http.Request, accessToken string) {
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "AITS-Community")
}

func randomToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func onlyMethod(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler(w, r)
	}
}

type apiActivityStatusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *apiActivityStatusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *apiActivityStatusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

func (a *appServer) requestActivityLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/ws/") {
			started := time.Now()
			log.Printf("[WS] OPEN %s", path)
			next.ServeHTTP(w, r)
			log.Printf("[WS] CLOSED %s (%s)", path, time.Since(started).Round(time.Millisecond))
			return
		}
		if !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		recorder := &apiActivityStatusWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		if recorder.status == 0 {
			recorder.status = http.StatusOK
		}
		if path != "/api/health" && path != "/api/admin/developer/system" {
			log.Printf("[API] %s %s -> %d · %d B · %s", r.Method, path, recorder.status, recorder.bytes, time.Since(started).Round(time.Millisecond))
		}
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
