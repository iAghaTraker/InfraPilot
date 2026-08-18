// Package bridge exposes the narrow localhost signing operation used by the
// Web Panel. It never returns or accepts private key material.
package bridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/identity"
)

const DefaultAddress = "127.0.0.1:8091"
const DefaultSocket = "/run/infrapilot/agent.sock"

func Serve(ctx context.Context, address, identityDir string) error {
	if _, err := os.Stat(DefaultSocket); err == nil {
		_ = os.Remove(DefaultSocket)
	}
	ln, err := net.Listen("unix", DefaultSocket)
	if err == nil {
		_ = os.Chmod(DefaultSocket, 0o660)
		return serveListener(ctx, ln, identityDir)
	}
	ln, err = net.Listen("tcp4", address)
	if err != nil {
		return err
	}
	return serveListener(ctx, ln, identityDir)
}

func serveListener(ctx context.Context, ln net.Listener, identityDir string) error {
	defer ln.Close()
	id, err := identity.Load(identityDir)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/sign", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		var req struct {
			Challenge string `json:"challenge"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		value, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(req.Challenge))
		if err != nil || len(value) != 32 {
			http.Error(w, "invalid challenge", 400)
			return
		}
		sig, err := id.Sign(value)
		if err != nil {
			http.Error(w, "signing unavailable", 503)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"signature": identity.EncodeSignature(sig)})
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); _ = srv.Shutdown(context.Background()) }()
	return srv.Serve(ln)
}
