package app

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type publicSyncHub struct {
	mu        sync.Mutex
	clients   map[*websocket.Conn]struct{}
	publicURL string
	upgrader  websocket.Upgrader
}

func newPublicSyncHub(publicURL string) *publicSyncHub {
	h := &publicSyncHub{clients: make(map[*websocket.Conn]struct{}), publicURL: publicURL}
	h.upgrader = websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 2048, CheckOrigin: h.originAllowed}
	return h
}

func (h *publicSyncHub) originAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "127.0.0.1" {
		return true
	}
	public, err := url.Parse(h.publicURL)
	return err == nil && strings.EqualFold(host, public.Hostname())
}

func (h *publicSyncHub) broadcast(scope string) {
	payload := map[string]any{
		"type":  "content_changed",
		"scope": strings.TrimSpace(scope),
		"time":  time.Now().Format(time.RFC3339Nano),
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		if err := conn.WriteJSON(payload); err != nil {
			_ = conn.Close()
			delete(h.clients, conn)
		}
	}
}

func (a *appServer) publicContentSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := a.publicSync.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	if err := conn.WriteJSON(map[string]any{"type": "ready", "time": time.Now().Format(time.RFC3339Nano)}); err != nil {
		_ = conn.Close()
		return
	}
	a.publicSync.mu.Lock()
	a.publicSync.clients[conn] = struct{}{}
	a.publicSync.mu.Unlock()
	defer func() {
		a.publicSync.mu.Lock()
		delete(a.publicSync.clients, conn)
		a.publicSync.mu.Unlock()
		_ = conn.Close()
	}()
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

type responseStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseStatusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseStatusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (a *appServer) withPublicRefresh(scope string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := &responseStatusWriter{ResponseWriter: w}
		handler(status, r)
		if status.status == 0 {
			status.status = http.StatusOK
		}
		if status.status < 200 || status.status >= 300 {
			return
		}
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			a.publicSync.broadcast(scope)
		}
	}
}
