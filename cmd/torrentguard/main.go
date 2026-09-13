package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"torrentguard/internal/config"
	"torrentguard/internal/qbit"
	"torrentguard/internal/service"
	"torrentguard/internal/state"
	"torrentguard/internal/webui"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0750); err != nil {
		logger.Error("cannot create data directory", "error", err)
		os.Exit(1)
	}
	store, err := state.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("cannot open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	qb := qbit.New(cfg.QBitURL, cfg.QBitUsername, cfg.QBitPassword, cfg.QBitAPIKey, cfg.QBitAuthMode)
	svc := service.New(cfg, qb, store, logger)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	mux := http.NewServeMux()
	var setupMu sync.Mutex
	setupAvailable := cfg.NeedsSetup
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if cfg.NeedsSetup {
				http.Error(w, "initial setup required", http.StatusServiceUnavailable)
				return
			}
			if cfg.UIPassword == "" {
				next(w, r)
				return
			}
			username, password, ok := r.BasicAuth()
			userOK := subtle.ConstantTimeCompare([]byte(username), []byte(cfg.UIUsername)) == 1
			passOK := subtle.ConstantTimeCompare([]byte(password), []byte(cfg.UIPassword)) == 1
			if !ok || !userOK || !passOK {
				w.Header().Set("WWW-Authenticate", `Basic realm="Torrent Payload Guard"`)
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if cfg.NeedsSetup {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		auth(webui.Dashboard)(w, r)
	})
	mux.HandleFunc("GET /setup", func(w http.ResponseWriter, r *http.Request) {
		setupMu.Lock()
		available := setupAvailable
		setupMu.Unlock()
		if !available {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		webui.Setup(w, r)
	})
	mux.HandleFunc("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		setupMu.Lock()
		defer setupMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !setupAvailable {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"initial setup is already complete"}`))
			return
		}
		var incoming config.Settings
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&incoming); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid request"}`))
			return
		}
		if err := config.SaveInitialSettings(cfg, incoming); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		setupAvailable = false
		_, _ = w.Write([]byte(`{"status":"saved"}`))
		go func() {
			time.Sleep(750 * time.Millisecond)
			cancel()
		}()
	})
	mux.HandleFunc("GET /settings", auth(webui.Settings))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /status", auth(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "running", "action_mode": cfg.ActionMode, "dry_run": cfg.DryRun,
			"poll_interval_seconds": int(cfg.PollInterval.Seconds()), "counters": svc.M.Snapshot(),
		})
	}))
	mux.HandleFunc("GET /api/torrents", auth(func(w http.ResponseWriter, r *http.Request) {
		records, err := store.List(r.Context(), 100)
		if err != nil {
			http.Error(w, `{"error":"database unavailable"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(records)
	}))
	mux.HandleFunc("GET /api/settings", auth(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"QBitURL": cfg.QBitURL, "QBitAuthMode": cfg.QBitAuthMode,
			"QBitUsername": cfg.QBitUsername, "QBitPassword": "", "QBitAPIKey": "",
			"SonarrURL": cfg.SonarrURL, "SonarrAPIKey": "", "RadarrURL": cfg.RadarrURL,
			"RadarrAPIKey": "", "PollIntervalSeconds": int(cfg.PollInterval.Seconds()),
			"PauseUnmapped": cfg.PauseUnmapped,
		})
	}))
	mux.HandleFunc("POST /api/settings", auth(func(w http.ResponseWriter, r *http.Request) {
		var incoming config.Settings
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&incoming); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}
		if err := config.SaveSettings(cfg, incoming); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"saved"}`))
		go func() {
			time.Sleep(750 * time.Millisecond)
			cancel()
		}()
	}))
	server := &http.Server{Addr: cfg.ListenAddress, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("status server started", "listen_address", cfg.ListenAddress)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("status server failed", "error", err)
			cancel()
		}
	}()

	if cfg.NeedsSetup {
		logger.Info("initial setup required", "setup_url", "/setup")
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = server.Shutdown(shutdownCtx)
		shutdownCancel()
		logger.Info("service stopped")
		return
	}
	if err := qb.Login(ctx); err != nil {
		logger.Warn("initial qBittorrent login failed; polling will retry", "error", err)
	}
	runScan := func() {
		scanCtx, scanCancel := context.WithTimeout(ctx, cfg.PollInterval)
		defer scanCancel()
		if err := svc.Scan(scanCtx); err != nil {
			logger.Error("poll failed", "error", err)
		}
	}
	runScan()
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = server.Shutdown(shutdownCtx)
			shutdownCancel()
			logger.Info("service stopped")
			return
		case <-ticker.C:
			runScan()
		}
	}
}
