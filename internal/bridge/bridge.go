// Package bridge exposes the narrow localhost signing operation used by the
// Web Panel. It never returns or accepts private key material.
package bridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
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
	var listeners []net.Listener
	if _, err := os.Stat(DefaultSocket); err == nil {
		_ = os.Remove(DefaultSocket)
	}
	ln, err := net.Listen("unix", DefaultSocket)
	if err == nil {
		_ = os.Chmod(DefaultSocket, 0o660)
		listeners = append(listeners, ln)
		slog.Info("agent bridge connected", "transport", "unix", "address", DefaultSocket)
	}
	tcp, tcpErr := net.Listen("tcp4", address)
	if tcpErr == nil {
		listeners = append(listeners, tcp)
		slog.Info("agent bridge connected", "transport", "tcp", "address", address)
	}
	if len(listeners) == 0 {
		if err != nil {
			return err
		}
		return tcpErr
	}
	errCh := make(chan error, len(listeners))
	for _, listener := range listeners {
		go func(l net.Listener) { errCh <- serveListener(ctx, l, identityDir) }(listener)
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func serveListener(ctx context.Context, ln net.Listener, identityDir string) error {
	defer ln.Close()
	id, err := identity.Load(identityDir)
	if err != nil {
		return err
	}
	mux := bridgeHandler(id)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	return srv.Serve(ln)
}

func bridgeHandler(id *identity.Identity) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/identity", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"device_id": id.DeviceID})
	})
	mux.HandleFunc("/sign", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
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
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"signature": identity.EncodeSignature(sig)})
	})
	return mux
}
