// Package web provides the local Web Panel HTTP foundation.
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/config"
	"github.com/iAghaTraker/InfraPilot/internal/core"
	"github.com/iAghaTraker/InfraPilot/internal/identity"
	"github.com/iAghaTraker/InfraPilot/internal/system"
)

const DefaultAddress = "127.0.0.1:8090"

type Server struct {
	http     *http.Server
	auth     *identity.Authenticator
	sessions map[string]time.Time
	mu       sync.Mutex
	cfg      config.Config
	paths    system.Paths
}

func New(cfg config.Config, paths system.Paths, repo *identity.Repository) *Server {
	s := &Server{cfg: cfg, paths: paths, auth: identity.NewAuthenticator(repo), sessions: make(map[string]time.Time)}
	bind := cfg.Web.BindAddress
	if bind == "" {
		bind = "127.0.0.1"
	}
	s.http = &http.Server{Addr: net.JoinHostPort(bind, "8090"), Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	return s
}
func (s *Server) ListenAndServe() error              { return s.http.ListenAndServe() }
func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(Assets, "dist/assets")
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("/api/auth/challenge", s.challenge)
	mux.HandleFunc("/api/auth/discover", s.discover)
	mux.HandleFunc("/api/auth/sign", s.sign)
	mux.HandleFunc("/api/diagnostics", s.diagnostics)
	mux.HandleFunc("/api/auth/verify", s.verify)
	mux.HandleFunc("/api/auth/logout", s.logout)
	mux.Handle("/api/status", s.protected(http.HandlerFunc(s.status)))
	mux.Handle("/api/system", s.protected(http.HandlerFunc(s.system)))
	mux.Handle("/api/services", s.protected(http.HandlerFunc(s.services)))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := Assets.ReadFile("dist/index.html")
		if err != nil {
			http.Error(w, "panel unavailable", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
	return mux
}
func bridgeHTTP() *http.Client {
	return &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/run/infrapilot/agent.sock")
	}}}
}
func bridgeRequest(r *http.Request, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.Context(), method, "http://agent"+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return bridgeHTTP().Do(req)
}
func (s *Server) discover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	resp, err := bridgeRequest(r, http.MethodGet, "/identity", nil)
	if err != nil || resp.StatusCode != http.StatusOK {
		slog.Warn("identity discovery failed", "reason", err.Error())
		writeJSON(w, map[string]any{"available": false, "reason": "identity unavailable"})
		return
	}
	defer resp.Body.Close()
	var found struct {
		DeviceID string `json:"device_id"`
	}
	if json.NewDecoder(resp.Body).Decode(&found) != nil || found.DeviceID == "" {
		writeJSON(w, map[string]any{"available": false, "reason": "identity unavailable"})
		return
	}
	slog.Info("identity storage found", "device_id", found.DeviceID)
	writeJSON(w, map[string]any{"available": true, "device_id": found.DeviceID})
}
func (s *Server) sign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	resp, err := bridgeRequest(r, http.MethodPost, "/sign", bytes.NewReader(b))
	if err != nil {
		http.Error(w, "agent bridge unavailable", 503)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
func (s *Server) diagnostics(w http.ResponseWriter, r *http.Request) {
	bridgeStatus, identityStatus := "unavailable", "unavailable"
	if resp, err := bridgeRequest(r, http.MethodGet, "/identity", nil); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			bridgeStatus = "connected"
			var v struct {
				DeviceID string `json:"device_id"`
			}
			if json.NewDecoder(resp.Body).Decode(&v) == nil && v.DeviceID != "" {
				identityStatus = "discovered"
			}
		}
	}
	writeJSON(w, map[string]string{"api": "connected", "bridge": bridgeStatus, "identity": identityStatus})
}
func (s *Server) protected(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		s.mu.Lock()
		expiry, ok := s.sessions[token]
		if ok && time.Now().After(expiry) {
			delete(s.sessions, token)
			ok = false
		}
		s.mu.Unlock()
		if token == "" || !ok {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) challenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil || req.DeviceID == "" {
		http.Error(w, "invalid request", 400)
		return
	}
	id, value, err := s.auth.Challenge(r.Context(), req.DeviceID, time.Now().UTC())
	if err != nil {
		http.Error(w, "authentication unavailable", 403)
		return
	}
	writeJSON(w, map[string]string{"challenge_id": id, "challenge": base64.RawURLEncoding.EncodeToString(value)})
}
func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		ChallengeID string `json:"challenge_id"`
		Signature   string `json:"signature"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	sig, err := base64.RawURLEncoding.DecodeString(req.Signature)
	if err != nil || req.ChallengeID == "" || s.auth.Verify(r.Context(), req.ChallengeID, sig, time.Now().UTC()) != nil {
		http.Error(w, "authentication failed", 401)
		return
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		http.Error(w, "authentication unavailable", 500)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(15 * time.Minute)
	s.mu.Unlock()
	writeJSON(w, map[string]string{"session": token})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, core.CollectStatus(r.Context(), s.cfg, s.paths, nil))
}
func (s *Server) system(w http.ResponseWriter, r *http.Request) { writeJSON(w, system.Collect()) }
func (s *Server) services(w http.ResponseWriter, r *http.Request) {
	items, err := core.Services(r.Context())
	if err != nil {
		http.Error(w, "service inspection failed", 500)
		return
	}
	writeJSON(w, items)
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func AddressAvailable(address string) error { _, _, err := net.SplitHostPort(address); return err }
