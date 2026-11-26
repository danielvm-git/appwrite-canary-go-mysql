package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/model"
	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/share"
	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/store"
)

type fakeRepo struct {
	tasks map[string]model.Task
	lists []model.List
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{tasks: map[string]model.Task{
		"t1": {ID: "t1", UserID: "owner", Title: "mine", Status: model.StatusOpen},
		"t2": {ID: "t2", UserID: "owner", Title: "shared", Status: model.StatusOpen, SharedWith: []string{"collab"}},
		"t3": {ID: "t3", UserID: "other", Title: "not yours", Status: model.StatusOpen},
	}}
}

func (f *fakeRepo) ListTasks(_ context.Context, filter store.TaskFilter) ([]model.Task, error) {
	out := []model.Task{}
	for _, t := range f.tasks {
		if t.UserID == filter.UserID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeRepo) GetTask(_ context.Context, id string) (model.Task, error) {
	t, ok := f.tasks[id]
	if !ok {
		return model.Task{}, store.ErrNotFound
	}
	return t, nil
}

func (f *fakeRepo) CreateTask(_ context.Context, id, ownerID, _ string, in model.TaskInput) (model.Task, error) {
	t := model.Task{ID: id, UserID: ownerID, Title: in.Title, Status: in.Status}
	f.tasks[id] = t
	return t, nil
}

func (f *fakeRepo) UpdateTask(_ context.Context, id string, patch model.TaskPatch) (model.Task, error) {
	t, ok := f.tasks[id]
	if !ok {
		return model.Task{}, store.ErrNotFound
	}
	if patch.Status != nil {
		t.Status = *patch.Status
	}
	if patch.Title != nil {
		t.Title = *patch.Title
	}
	f.tasks[id] = t
	return t, nil
}

func (f *fakeRepo) DeleteTask(_ context.Context, id string) error {
	if _, ok := f.tasks[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.tasks, id)
	return nil
}

func (f *fakeRepo) Shares(context.Context, string) ([]store.Share, error)  { return nil, nil }
func (f *fakeRepo) AddShare(context.Context, string, string, string) error { return nil }

func (f *fakeRepo) ListLists(context.Context, string) ([]model.List, error) { return f.lists, nil }
func (f *fakeRepo) CreateList(_ context.Context, id, userID string, in model.ListInput) (model.List, error) {
	l := model.List{ID: id, Name: in.Name, Color: in.Color, UserID: userID}
	f.lists = append(f.lists, l)
	return l, nil
}
func (f *fakeRepo) DeleteList(context.Context, string, string) error { return nil }

func newServer() (*Server, *fakeRepo) {
	repo := newFakeRepo()
	return New(Deps{
		Tasks:        repo,
		Lists:        repo,
		Users:        func(*http.Request) share.Users { return nil },
		AllowDevUser: true,
		Version:      "test",
	}), repo
}

func do(t *testing.T, s *Server, method, path, actor string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if actor != "" {
		req.Header.Set("x-dev-user", actor)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestAnonymousIsRejected(t *testing.T) {
	s, _ := newServer()
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/tasks"},
		{http.MethodPost, "/tasks"},
		{http.MethodGet, "/tasks/t1"},
		{http.MethodPatch, "/tasks/t1"},
		{http.MethodDelete, "/tasks/t1"},
		{http.MethodGet, "/lists"},
		{http.MethodPost, "/share"},
	} {
		rec := do(t, s, tc.method, tc.path, "", map[string]any{})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: status = %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestOwnerCanReadAndWrite(t *testing.T) {
	s, _ := newServer()

	if rec := do(t, s, http.MethodGet, "/tasks/t1", "owner", nil); rec.Code != http.StatusOK {
		t.Fatalf("read: %d", rec.Code)
	}
	rec := do(t, s, http.MethodPatch, "/tasks/t1", "owner", map[string]any{"title": "renamed", "status": "doing"})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, s, http.MethodDelete, "/tasks/t1", "owner", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
}

func TestCollaboratorCanReadAndMoveStatusOnly(t *testing.T) {
	s, _ := newServer()

	if rec := do(t, s, http.MethodGet, "/tasks/t2", "collab", nil); rec.Code != http.StatusOK {
		t.Fatalf("collaborator read: %d", rec.Code)
	}
	if rec := do(t, s, http.MethodPatch, "/tasks/t2", "collab", map[string]any{"status": "review"}); rec.Code != http.StatusOK {
		t.Fatalf("collaborator status patch: %d", rec.Code)
	}
	// The gap the Appwrite implementation documents: a raw PATCH could touch other columns.
	if rec := do(t, s, http.MethodPatch, "/tasks/t2", "collab", map[string]any{"title": "hijack"}); rec.Code != http.StatusForbidden {
		t.Fatalf("collaborator title patch must be forbidden, got %d", rec.Code)
	}
	if rec := do(t, s, http.MethodDelete, "/tasks/t2", "collab", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("collaborator delete must be forbidden, got %d", rec.Code)
	}
}

func TestStrangerCannotRead(t *testing.T) {
	s, _ := newServer()
	if rec := do(t, s, http.MethodGet, "/tasks/t3", "collab", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("stranger read: %d", rec.Code)
	}
}

func TestValidation(t *testing.T) {
	s, _ := newServer()

	if rec := do(t, s, http.MethodPost, "/tasks", "owner", map[string]any{"title": "  "}); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty title: %d", rec.Code)
	}
	if rec := do(t, s, http.MethodPost, "/tasks", "owner", map[string]any{"title": "x", "status": "nope"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad status: %d", rec.Code)
	}
	if rec := do(t, s, http.MethodPost, "/lists", "owner", map[string]any{"name": ""}); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty list name: %d", rec.Code)
	}
	if rec := do(t, s, http.MethodGet, "/tasks/missing", "owner", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing task: %d", rec.Code)
	}
}

func TestHealth(t *testing.T) {
	s, _ := newServer()
	rec := do(t, s, http.MethodGet, "/health", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("health: %d", rec.Code)
	}
}
