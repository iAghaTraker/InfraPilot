package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/iAghaTraker/InfraPilot/internal/config"
	"github.com/iAghaTraker/InfraPilot/internal/identity"
	"github.com/iAghaTraker/InfraPilot/internal/storage"
	"github.com/iAghaTraker/InfraPilot/internal/system"
	"github.com/iAghaTraker/InfraPilot/internal/web"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	paths, err := system.ResolvePaths()
	if err != nil {
		fatal(err)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		fatal(err)
	}
	dbPath, err := cfg.DatabasePath()
	if err != nil {
		fatal(err)
	}
	db, err := storage.Open(ctx, storage.Options{Path: dbPath, BusyTimeout: cfg.Storage.BusyTimeout})
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	server := web.New(cfg, paths, identity.NewRepository(db))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "infrapilot-web: %v\n", err)
	os.Exit(1)
}
