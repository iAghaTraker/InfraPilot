package web

import (
	"bytes"
	"encoding/json"
	"github.com/iAghaTraker/InfraPilot/internal/config"
	"github.com/iAghaTraker/InfraPilot/internal/identity"
	"github.com/iAghaTraker/InfraPilot/internal/storage"
	"github.com/iAghaTraker/InfraPilot/internal/system"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func testServer(t *testing.T) (*Server, *identity.Identity) {
	dir := t.TempDir()
	db, err := storage.Open(t.Context(), storage.Options{Path: filepath.Join(dir, "db.sqlite"), BusyTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	id, _, err := identity.Create(filepath.Join(dir, "identity"))
	if err != nil {
		t.Fatal(err)
	}
	token, _ := id.PairingToken(time.Now())
	repo := identity.NewRepository(db)
	if _, err := repo.Replace(t.Context(), token, time.Now()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Agent.DataDir = dir
	return New(cfg, system.Paths{DataDir: dir}, repo), id
}
func TestProtectedAPIsRejectUnauthenticatedRequests(t *testing.T) {
	s, _ := testServer(t)
	for _, path := range []string{"/api/status", "/api/system", "/api/services"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		s.http.Handler.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s status=%d", path, w.Code)
		}
	}
}

func TestEmbeddedUIIsServed(t *testing.T) {
	s, _ := testServer(t)
	w := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte("InfraPilot")) {
		t.Fatalf("UI status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/assets/app.css", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("asset status=%d", w.Code)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	s, _ := testServer(t)
	s.sessions["expired"] = time.Now().Add(-time.Second)
	r := httptest.NewRequest("GET", "/api/system", nil)
	r.Header.Set("Authorization", "Bearer expired")
	w := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status=%d", w.Code)
	}
}
func TestChallengeVerificationCreatesSession(t *testing.T) {
	s, id := testServer(t)
	body, _ := json.Marshal(map[string]string{"device_id": id.DeviceID})
	w := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/challenge", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("challenge status=%d body=%s", w.Code, w.Body.String())
	}
	var challenge struct {
		ID    string `json:"challenge_id"`
		Value string `json:"challenge"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &challenge)
	raw, err := identity.DecodeChallenge(challenge.Value)
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := id.Sign(raw)
	verify, _ := json.Marshal(map[string]string{"challenge_id": challenge.ID, "signature": identity.EncodeSignature(sig)})
	w = httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/verify", bytes.NewReader(verify)))
	if w.Code != 200 {
		t.Fatalf("verify status=%d body=%s", w.Code, w.Body.String())
	}
	var session map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &session)
	r := httptest.NewRequest("GET", "/api/system", nil)
	r.Header.Set("Authorization", "Bearer "+session["session"])
	w = httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("authorized status=%d", w.Code)
	}
}
func TestInvalidRequests(t *testing.T) {
	s, _ := testServer(t)
	w := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/auth/challenge", nil))
	if w.Code != 405 {
		t.Fatalf("method status=%d", w.Code)
	}
	w = httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/challenge", bytes.NewBufferString("{}")))
	if w.Code != 400 {
		t.Fatalf("invalid status=%d", w.Code)
	}
}
