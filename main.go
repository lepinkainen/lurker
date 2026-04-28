package main

import (
	"context"
	"errors"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lepinkainen/lurker/api"
	"github.com/lepinkainen/lurker/db"
	"github.com/lepinkainen/lurker/hub"
	"github.com/lepinkainen/lurker/internal/closeutil"
	"github.com/lepinkainen/lurker/irc"
	"github.com/lepinkainen/lurker/preview"
	"github.com/lepinkainen/lurker/theme"
	"github.com/lepinkainen/lurker/updates"
)

func main() {
	webDir := flag.String("web-dir", "", "serve built web UI from this directory")
	themesDir := flag.String("themes-dir", "", "load user themes from this directory (overrides THEMES_DIR)")
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
	previewSvc := preview.NewService(preview.Config{
		Enabled:  cfg.Previews.Enabled,
		MaxBytes: int64(cfg.Previews.MaxBytes),
		Timeout:  time.Duration(cfg.Previews.TimeoutMs) * time.Millisecond,
		CacheTTL: time.Duration(cfg.Previews.CacheTTLHours) * time.Hour,
		Workers:  cfg.Previews.Workers,
	}, stores, evHub)
	previewSvc.Start(ctx)
	defer closeutil.Ignore(previewSvc, "component", "preview")
	mgr := irc.NewManager(stores, evHub)
	mgr.SetPreviewEnqueuer(previewSvc)
	if nets := cfg.Networks; len(nets) > 0 {
		startErr := mgr.Start(ctx, nets)
		if startErr != nil {
			slog.Error("start bootstrap networks", "err", startErr)
			os.Exit(1)
		}
		slog.Info("irc bootstrap networks started", "count", len(nets))
		// Mark DB networks absent from config.yaml as disabled so they don't
		// auto-connect and are visually distinguished in the UI.
		yamlNames := make([]string, len(nets))
		for i, n := range nets {
			yamlNames[i] = n.Name
		}
		if err := db.MarkNonYAMLNetworksDisabled(ctx, stores.Control, yamlNames); err != nil {
			slog.Error("mark non-yaml networks disabled", "err", err)
		}
	} else {
		slog.Info("no bootstrap networks configured; add networks to config.yaml to seed control.db on startup")
	}

	var webSub fs.FS
	if *webDir != "" {
		webSub = os.DirFS(*webDir)
		if _, statErr := fs.Stat(webSub, "index.html"); statErr != nil {
			slog.Error("web dir missing index.html", "dir", *webDir, "err", statErr)
			os.Exit(1)
		}
		slog.Info("web UI served from disk", "dir", *webDir)
	} else {
		slog.Warn("web UI disabled", "hint", "pass --web-dir ./web/dist or run container image")
	}

	resolvedThemesDir := *themesDir
	if resolvedThemesDir == "" {
		resolvedThemesDir = cfg.ThemesDir
	}
	themeLoader := &theme.Loader{Dir: resolvedThemesDir}
	slog.Info("themes", "dir", resolvedThemesDir)

	updateChecker := updates.New(updates.Config{
		Enabled:  cfg.Updates.Enabled,
		Image:    cfg.Updates.Image,
		Tag:      cfg.Updates.Tag,
		Interval: cfg.Updates.Interval,
		Username: cfg.Updates.Username,
		Token:    cfg.Updates.Token,
		Current: updates.BuildInfo{
			Version:   version,
			Commit:    gitHash,
			BuildTime: buildTime,
		},
	})
	updateChecker.Start(ctx)

	apiSrv := &api.Server{
		Stores:        stores,
		Hub:           evHub,
		Manager:       mgr,
		Web:           webSub,
		Themes:        themeLoader,
		AppName:       appName,
		Version:       version,
		GitHash:       gitHash,
		BuildTime:     buildTime,
		UpdateChecker: updateChecker,
		ConfigPreview: func(ctx context.Context) (string, string, error) {
			nets, err := db.ListNetworksWithSASL(ctx, stores.Control)
			if err != nil {
				return "", "", err
			}
			return previewConfigYAML(cfg.ConfigPath, nets)
		},
		ConfigSave: func(_ context.Context, content string) error {
			return saveConfigYAML(cfg.ConfigPath, content)
		},
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
