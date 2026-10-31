// Package api serves the notifier's HTTP API for the app: one-time links
// that connect a Telegram chat to a tenant, and the chats connected. Its
// contract is api/openapi.yaml.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jobloom/candylario/notifier/internal/store"
)

type Server struct {
	// Token opens every route but /v1/health.
	Token string
	// Bot is the bot's username, for the links.
	Bot   string
	Store *store.Store
	Logf  func(format string, args ...any)
	// TTL is how long a link works; 10 minutes by default.
	TTL time.Duration
	// Commit is the source commit the notifier was built from, for the
	// health check.
	Commit string
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.health)
	mux.HandleFunc("POST /v1/tenants/{tenant}/links", s.auth(s.link))
	mux.HandleFunc("GET /v1/tenants/{tenant}/chats", s.auth(s.chats))
	mux.HandleFunc("DELETE /v1/tenants/{tenant}/chats/{chat}", s.auth(s.unlink))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "not_found", "no such route")
	})
	return mux
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code, message string) {
	write(w, status, map[string]apiError{"error": {Code: code, Message: message}})
}

func (s *Server) auth(next func(http.ResponseWriter, *http.Request, uuid.UUID)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || s.Token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) != 1 {
			fail(w, http.StatusUnauthorized, "unauthorized", "a valid bearer token is required")
			return
		}
		tenant, err := uuid.Parse(r.PathValue("tenant"))
		if err != nil {
			fail(w, http.StatusBadRequest, "bad_request", "tenant must be a UUID")
			return
		}
		next(w, r, tenant)
	}
}

func (s *Server) internal(w http.ResponseWriter, err error) {
	if s.Logf != nil {
		s.Logf("api: %v", err)
	}
	fail(w, http.StatusInternalServerError, "internal", "something went wrong")
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		fail(w, http.StatusServiceUnavailable, "internal", "database unavailable")
		return
	}
	write(w, http.StatusOK, map[string]any{"ok": true, "commit": s.Commit})
}

func (s *Server) link(w http.ResponseWriter, r *http.Request, tenant uuid.UUID) {
	ttl := s.TTL
	if ttl == 0 {
		ttl = 10 * time.Minute
	}
	code, err := s.Store.NewCode(r.Context(), tenant, ttl)
	if err != nil {
		s.internal(w, err)
		return
	}
	write(w, http.StatusCreated, map[string]string{
		"url":        "https://t.me/" + s.Bot + "?start=" + code,
		"expires_at": time.Now().Add(ttl).UTC().Format(time.RFC3339),
	})
}

type chat struct {
	ChatID   int64     `json:"chat_id"`
	Name     string    `json:"name"`
	LinkedAt time.Time `json:"linked_at"`
}

func (s *Server) chats(w http.ResponseWriter, r *http.Request, tenant uuid.UUID) {
	links, err := s.Store.LinksOf(r.Context(), tenant)
	if err != nil {
		s.internal(w, err)
		return
	}
	out := make([]chat, 0, len(links))
	for _, l := range links {
		out = append(out, chat{ChatID: l.Chat, Name: l.Name, LinkedAt: l.LinkedAt.UTC()})
	}
	write(w, http.StatusOK, map[string][]chat{"chats": out})
}

func (s *Server) unlink(w http.ResponseWriter, r *http.Request, tenant uuid.UUID) {
	id, err := strconv.ParseInt(r.PathValue("chat"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_request", "chat must be an integer")
		return
	}
	ok, err := s.Store.UnlinkOf(r.Context(), tenant, id)
	if err != nil {
		s.internal(w, err)
		return
	}
	if !ok {
		fail(w, http.StatusNotFound, "not_found", "this chat is not connected to the tenant")
		return
	}
	write(w, http.StatusOK, map[string]bool{"ok": true})
}

// Serve answers on addr until ctx ends.
func (s *Server) Serve(ctx context.Context, addr string) error {
	hs := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := hs.Shutdown(sctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
