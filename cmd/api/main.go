// Command api is the Todo data API backed by Oracle MySQL HeatWave.
//
// As an Appwrite Function it binds 0.0.0.0:${PORT:-3000} and trusts the
// x-appwrite-user-id header the runtime injects. Locally it is the same server
// with ALLOW_DEV_USER=true so `curl -H 'x-dev-user: u1'` can drive it.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"openruntimes/handler/pkg/api"
	"openruntimes/handler/pkg/appwrite"
	"openruntimes/handler/pkg/config"
	"openruntimes/handler/pkg/db"
	"openruntimes/handler/pkg/schema"
	"openruntimes/handler/pkg/share"
	"openruntimes/handler/pkg/store"
)

// buildVersion is injected with -ldflags "-X main.buildVersion=$(VERSION)".
var buildVersion = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	version := buildVersion
	if data, err := os.ReadFile("VERSION"); err == nil {
		if v := trimSpace(string(data)); v != "" {
			version = v
		}
	}

	cfg := config.Load()

	pool, err := db.Open(cfg)
	if err != nil {
		logger.Error("cannot reach MySQL HeatWave", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	repo := store.NewMySQL(pool)

	users := func(r *http.Request) share.Users {
		key := r.Header.Get("x-appwrite-key")
		if key == "" {
			key = cfg.AppwriteAPIKey
		}
		return appwrite.New(cfg.AppwriteEndpoint, cfg.AppwriteProjectID, key)
	}

	server := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: api.New(api.Deps{
			Tasks:        repo,
			Lists:        repo,
			Users:        users,
			AllowDevUser: cfg.AllowDevUser,
			Version:      version,
			Logger:       logger,
			Migrate: func(ctx context.Context) (any, error) {
				return schema.Apply(ctx, pool)
			},
			MigrateToken: cfg.MigrateToken,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	go func() {
		logger.Info("todo-api-mysql listening", "addr", server.Addr, "version", version,
			"db", cfg.DBHost, "schema", cfg.DBName, "dev_user", cfg.AllowDevUser)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
	}
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\n' || s[start] == '\r' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\n' || s[end-1] == '\r' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
