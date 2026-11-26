// Package api is the HTTP surface. It runs unchanged as an Appwrite Function
// (the runtime gives it 0.0.0.0:3000 and injects x-appwrite-user-id) and as a
// plain local server for tests.
package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/model"
	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/share"
	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/store"
)

// UsersFactory builds the e-mail resolver for the current request, so the
// per-invocation Appwrite key can be threaded through.
type UsersFactory func(r *http.Request) share.Users

type Deps struct {
	Tasks        store.TaskStore
	Lists        store.ListStore
	Users        UsersFactory
	AllowDevUser bool
	Version      string
	Logger       *slog.Logger

	// Migrate applies the embedded schema. Deploys are CI-only, so this is the
	// only path that changes the database; the route is token-protected.
	Migrate      func(ctx context.Context) (any, error)
	MigrateToken string
}

type Server struct {
	Deps
	share *share.Service
	mux   *http.ServeMux
}

func New(d Deps) *Server {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	s := &Server{Deps: d}
	s.mux = http.NewServeMux()
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.health)

	s.mux.HandleFunc("GET /tasks", s.listTasks)
	s.mux.HandleFunc("POST /tasks", s.createTask)
	s.mux.HandleFunc("GET /tasks/{id}", s.getTask)
	s.mux.HandleFunc("PATCH /tasks/{id}", s.patchTask)
	s.mux.HandleFunc("DELETE /tasks/{id}", s.deleteTask)

	s.mux.HandleFunc("GET /lists", s.listLists)
	s.mux.HandleFunc("POST /lists", s.createList)
	s.mux.HandleFunc("DELETE /lists/{id}", s.deleteList)

	s.mux.HandleFunc("POST /share", s.shareTask)

	s.mux.HandleFunc("POST /admin/migrate", s.migrate)
}

// --- handlers --------------------------------------------------------------

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": s.Version,
		"role":    "todo-api-mysql",
		"actor":   s.actor(r),
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	actor := s.actor(r)
	if actor == "" {
		s.fail(w, r, http.StatusUnauthorized, share.CodeUnauthorized, "missing caller user id")
		return
	}
	scope := r.URL.Query().Get("scope")

	// "shared" and "<listId>" are the two scopes the UI sends besides the default.
	tasks, err := s.Tasks.ListTasks(r.Context(), store.TaskFilter{
		UserID: actor,
		Scope:  scope,
		Search: r.URL.Query().Get("q"),
	})
	if err != nil {
		s.failErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": len(tasks), "tasks": tasks})
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	actor := s.actor(r)
	if actor == "" {
		s.fail(w, r, http.StatusUnauthorized, share.CodeUnauthorized, "missing caller user id")
		return
	}

	var in model.TaskInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		s.fail(w, r, http.StatusBadRequest, share.CodeInvalid, "malformed JSON body")
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		s.fail(w, r, http.StatusBadRequest, share.CodeInvalid, "title is required")
		return
	}
	if in.Status == "" {
		in.Status = model.StatusOpen
	}
	if !oneOf(in.Status, model.ValidStatuses) {
		s.fail(w, r, http.StatusBadRequest, share.CodeInvalid, "invalid status")
		return
	}
	if in.Priority != "" && !oneOf(in.Priority, model.ValidPriorities) {
		s.fail(w, r, http.StatusBadRequest, share.CodeInvalid, "invalid priority")
		return
	}

	task, err := s.Tasks.CreateTask(r.Context(), newID(), actor, r.Header.Get("x-appwrite-user-name"), in)
	if err != nil {
		s.failErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	actor, task, ok := s.taskForActor(w, r)
	if !ok {
		return
	}
	if !canRead(task, actor) {
		s.fail(w, r, http.StatusForbidden, share.CodeForbidden, "no access to this task")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) patchTask(w http.ResponseWriter, r *http.Request) {
	actor, task, ok := s.taskForActor(w, r)
	if !ok {
		return
	}
	if !canRead(task, actor) {
		s.fail(w, r, http.StatusForbidden, share.CodeForbidden, "no access to this task")
		return
	}

	var patch model.TaskPatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&patch); err != nil {
		s.fail(w, r, http.StatusBadRequest, share.CodeInvalid, "malformed JSON body")
		return
	}
	if patch.Status != nil && !oneOf(*patch.Status, model.ValidStatuses) {
		s.fail(w, r, http.StatusBadRequest, share.CodeInvalid, "invalid status")
		return
	}
	if patch.Priority != nil && *patch.Priority != "" && !oneOf(*patch.Priority, model.ValidPriorities) {
		s.fail(w, r, http.StatusBadRequest, share.CodeInvalid, "invalid priority")
		return
	}
	if patch.Title != nil {
		trimmed := strings.TrimSpace(*patch.Title)
		if trimmed == "" {
			s.fail(w, r, http.StatusBadRequest, share.CodeInvalid, "title cannot be empty")
			return
		}
		patch.Title = &trimmed
	}

	// Collaborators may move the status and nothing else. This closes the gap the
	// Appwrite implementation documents (a raw PATCH could touch other columns).
	if task.UserID != actor {
		if onlyStatus(patch) == false {
			s.fail(w, r, http.StatusForbidden, share.CodeForbidden, "collaborators can only change the status")
			return
		}
	}

	updated, err := s.Tasks.UpdateTask(r.Context(), task.ID, patch)
	if err != nil {
		s.failErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	actor, task, ok := s.taskForActor(w, r)
	if !ok {
		return
	}
	if task.UserID != actor {
		s.fail(w, r, http.StatusForbidden, share.CodeForbidden, "only the owner can delete this task")
		return
	}
	if err := s.Tasks.DeleteTask(r.Context(), task.ID); err != nil {
		s.failErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listLists(w http.ResponseWriter, r *http.Request) {
	actor := s.actor(r)
	if actor == "" {
		s.fail(w, r, http.StatusUnauthorized, share.CodeUnauthorized, "missing caller user id")
		return
	}
	lists, err := s.Lists.ListLists(r.Context(), actor)
	if err != nil {
		s.failErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": len(lists), "lists": lists})
}

func (s *Server) createList(w http.ResponseWriter, r *http.Request) {
	actor := s.actor(r)
	if actor == "" {
		s.fail(w, r, http.StatusUnauthorized, share.CodeUnauthorized, "missing caller user id")
		return
	}
	var in model.ListInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&in); err != nil {
		s.fail(w, r, http.StatusBadRequest, share.CodeInvalid, "malformed JSON body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		s.fail(w, r, http.StatusBadRequest, share.CodeInvalid, "name is required")
		return
	}
	list, err := s.Lists.CreateList(r.Context(), newID(), actor, in)
	if err != nil {
		s.failErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, list)
}

func (s *Server) deleteList(w http.ResponseWriter, r *http.Request) {
	actor := s.actor(r)
	if actor == "" {
		s.fail(w, r, http.StatusUnauthorized, share.CodeUnauthorized, "missing caller user id")
		return
	}
	if err := s.Lists.DeleteList(r.Context(), r.PathValue("id"), actor); err != nil {
		s.failErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) shareTask(w http.ResponseWriter, r *http.Request) {
	actor := s.actor(r)
	if actor == "" {
		s.fail(w, r, http.StatusUnauthorized, share.CodeUnauthorized, "missing caller user id")
		return
	}
	var req model.ShareRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		s.fail(w, r, http.StatusBadRequest, share.CodeInvalid, "malformed JSON body")
		return
	}

	svc := share.New(s.Tasks, s.Users(r))
	resp, err := svc.Share(r.Context(), actor, req.TaskID, req.Email)
	if err != nil {
		var shareErr *share.Error
		if errors.As(err, &shareErr) {
			s.fail(w, r, statusFor(shareErr.Code), shareErr.Code, shareErr.Message)
			return
		}
		s.failErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// migrate applies the embedded migrations. CI calls it right after a deploy with
// the MIGRATE_TOKEN header; there is no other way to touch the schema.
func (s *Server) migrate(w http.ResponseWriter, r *http.Request) {
	if s.Migrate == nil || s.MigrateToken == "" {
		s.fail(w, r, http.StatusNotImplemented, "disabled", "migrations are not configured for this deployment")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("x-migrate-token")), []byte(s.MigrateToken)) != 1 {
		s.fail(w, r, http.StatusUnauthorized, share.CodeUnauthorized, "invalid migrate token")
		return
	}
	result, err := s.Migrate(r.Context())
	if err != nil {
		s.failErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "schema": result})
}

// --- helpers ---------------------------------------------------------------

func (s *Server) taskForActor(w http.ResponseWriter, r *http.Request) (string, model.Task, bool) {
	actor := s.actor(r)
	if actor == "" {
		s.fail(w, r, http.StatusUnauthorized, share.CodeUnauthorized, "missing caller user id")
		return "", model.Task{}, false
	}
	task, err := s.Tasks.GetTask(r.Context(), r.PathValue("id"))
	if err != nil {
		s.failErr(w, r, err)
		return "", model.Task{}, false
	}
	return actor, task, true
}

// actor reads the caller identity. Appwrite injects x-appwrite-user-id for
// authenticated executions; the dev header exists only when ALLOW_DEV_USER=true.
func (s *Server) actor(r *http.Request) string {
	if id := strings.TrimSpace(r.Header.Get("x-appwrite-user-id")); id != "" {
		return id
	}
	if s.AllowDevUser {
		return strings.TrimSpace(r.Header.Get("x-dev-user"))
	}
	return ""
}

func (s *Server) failErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.fail(w, r, http.StatusNotFound, share.CodeNotFound, "not found")
	default:
		s.Logger.Error("request failed", "path", r.URL.Path, "method", r.Method, "err", err)
		s.fail(w, r, http.StatusInternalServerError, "server_error", "internal error")
	}
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	if status >= 500 {
		s.Logger.Error("request failed", "path", r.URL.Path, "code", code, "message", message)
	}
	writeJSON(w, status, model.ErrorResponse{OK: false, Code: code, Message: message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func canRead(task model.Task, actor string) bool {
	if task.UserID == actor {
		return true
	}
	for _, id := range task.SharedWith {
		if id == actor {
			return true
		}
	}
	return false
}

func onlyStatus(p model.TaskPatch) bool {
	return p.Status != nil &&
		p.Title == nil && p.Notes == nil && p.Priority == nil &&
		p.DueDate == nil && p.ListID == nil && p.AttachmentID == nil
}

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if a == v {
			return true
		}
	}
	return false
}

func statusFor(code string) int {
	switch code {
	case share.CodeUnauthorized:
		return http.StatusUnauthorized
	case share.CodeInvalid:
		return http.StatusBadRequest
	case share.CodeNotFound, share.CodeNoUser:
		return http.StatusNotFound
	case share.CodeForbidden, share.CodeSelf, share.CodeAlready:
		return http.StatusForbidden
	default:
		return http.StatusBadRequest
	}
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failing is unrecoverable
	}
	b[6] = (b[6] & 0x0f) | 0x40 // v4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// ContextWithTimeout is exported for main() to bound graceful shutdown.
func ContextWithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}
