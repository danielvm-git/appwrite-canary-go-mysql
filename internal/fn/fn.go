// Package fn adapts the HTTP handler to the Open Runtimes Go contract used by
// Appwrite Functions (package handler + func Main(types.Context)).
//
// The Appwrite Go runtime is not a plain HTTP server: the runtime hands each
// invocation to Main, so the API is built once per container (pooling the MySQL
// connection) and every invocation is replayed as an *http.Request against the
// same handler the local server uses. One code path, two entrypoints.
package fn

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"

	types "github.com/open-runtimes/types-for-go/v4/openruntimes"

	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/api"
	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/appwrite"
	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/config"
	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/db"
	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/schema"
	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/share"
	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/store"
)

var (
	once    sync.Once
	handler http.Handler
	bootErr error
)

// Handle is the function entrypoint body: init once, then serve.
func Handle(ctx types.Context) types.Response {
	once.Do(boot)
	if bootErr != nil {
		return ctx.Res.Json(map[string]any{
			"ok":      false,
			"code":    "server_error",
			"message": bootErr.Error(),
		}, ctx.Res.WithStatusCode(http.StatusInternalServerError))
	}

	req, err := toRequest(ctx)
	if err != nil {
		return ctx.Res.Json(map[string]any{"ok": false, "code": "invalid", "message": err.Error()},
			ctx.Res.WithStatusCode(http.StatusBadRequest))
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	headers := map[string]string{}
	for key := range rec.Header() {
		headers[strings.ToLower(key)] = rec.Header().Get(key)
	}
	return ctx.Res.Binary(rec.Body.Bytes(), ctx.Res.WithStatusCode(rec.Code), ctx.Res.WithHeaders(headers))
}

func boot() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := config.Load()

	pool, err := db.Open(cfg)
	if err != nil {
		bootErr = err
		logger.Error("cannot reach MySQL HeatWave", "err", err)
		return
	}

	repo := store.NewMySQL(pool)
	handler = api.New(api.Deps{
		Tasks: repo,
		Lists: repo,
		Users: func(r *http.Request) share.Users {
			key := r.Header.Get("x-appwrite-key")
			if key == "" {
				key = cfg.AppwriteAPIKey
			}
			return appwrite.New(cfg.AppwriteEndpoint, cfg.AppwriteProjectID, key)
		},
		AllowDevUser: cfg.AllowDevUser,
		Version:      cfg.Version,
		Logger:       logger,
		Migrate: func(ctx context.Context) (any, error) {
			return schema.Apply(ctx, pool)
		},
		MigrateToken: cfg.MigrateToken,
	})
	logger.Info("function booted", "version", cfg.Version, "schema", cfg.DBName, "host", cfg.DBHost)
}

// toRequest replays the runtime invocation as an http.Request.
func toRequest(ctx types.Context) (*http.Request, error) {
	target := ctx.Req.Path
	if target == "" {
		target = "/"
	}
	if ctx.Req.QueryString != "" && !strings.Contains(target, "?") {
		target += "?" + ctx.Req.QueryString
	}
	req := httptest.NewRequest(ctx.Req.Method, target, bytes.NewReader(ctx.Req.BodyBinary()))
	for key, value := range ctx.Req.Headers {
		req.Header.Set(key, value)
	}
	req.RemoteAddr = "appwrite-function"
	return req, nil
}
