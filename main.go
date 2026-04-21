package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lepinkainen/research/irc-service/api"
	"github.com/lepinkainen/research/irc-service/db"
	"github.com/lepinkainen/research/irc-service/hub"
	"github.com/lepinkainen/research/irc-service/internal/closeutil"
	"github.com/lepinkainen/research/irc-service/irc"
)

//go:embed web
var webFS embed.FS

func main() {
	webDir := flag.String("web-dir", "", "serve web UI from this directory instead of the embedded copy")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cfg := loadConfig()

	stores, err := db.OpenMultiStore(cfg.DataDir)
	if err != nil {
		slog.Error("open stores", "err", err, "data_dir", cfg.DataDir)
		os.Exit(1)
	}
	defer closeutil.Ignore(stores, "component", "stores")
	slog.Info("stores ready", "control_db", cfg.ControlDBPath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownCtx, shutdownCancel := context.WithTimeoutCause(context.Background(), 10*time.Second, errors.New("shutdown timeout"))
	defer shutdownCancel()

	evHub := hub.New()
	mgr := irc.NewManager(stores, evHub)
	if nets := cfg.Networks; len(nets) > 0 {
		startErr := mgr.Start(ctx, nets)
		if startErr != nil {
			slog.Error("start bootstrap networks", "err", startErr)
			os.Exit(1)
		}
		slog.Info("irc bootstrap networks started", "count", len(nets))
	} else {
		slog.Info("no bootstrap networks configured; set CONFIG_PATH to seed control.db on startup")
	}

	var webSub fs.FS
	if *webDir != "" {
		webSub = os.DirFS(*webDir)
		slog.Info("web UI served from disk", "dir", *webDir)
	} else {
		webSub, err = fs.Sub(webFS, "web")
		if err != nil {
			slog.Error("web fs sub", "err", err)
			os.Exit(1)
		}
	}

	apiSrv := &api.Server{
		Stores:    stores,
		Hub:       evHub,
		Manager:   mgr,
		Web:       webSub,
		AppName:   appName,
		Version:   version,
		GitHash:   gitHash,
		BuildTime: buildTime,
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           apiSrv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("http listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http serve", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown", "err", err)
	}
	mgr.Wait()
	slog.Info("bye")
}
