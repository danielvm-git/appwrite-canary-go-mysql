package share

import (
	"context"
	"errors"
	"testing"

	"openruntimes/handler/pkg/model"
	"openruntimes/handler/pkg/store"
)

// fakeStore is a minimal in-memory store: the seven required cases do not need SQL.
type fakeStore struct {
	task      model.Task
	taskErr   error
	shares    []store.Share
	addErr    error
	addCalls  int
	getCalls  int
	shareRead int
}

func (f *fakeStore) ListTasks(context.Context, store.TaskFilter) ([]model.Task, error) {
	return nil, nil
}
func (f *fakeStore) GetTask(context.Context, string) (model.Task, error) {
	f.getCalls++
	if f.taskErr != nil {
		return model.Task{}, f.taskErr
	}
	return f.task, nil
}
func (f *fakeStore) CreateTask(context.Context, string, string, string, model.TaskInput) (model.Task, error) {
	return model.Task{}, nil
}
func (f *fakeStore) UpdateTask(context.Context, string, model.TaskPatch) (model.Task, error) {
	return model.Task{}, nil
}
func (f *fakeStore) DeleteTask(context.Context, string) error { return nil }
func (f *fakeStore) Shares(context.Context, string) ([]store.Share, error) {
	f.shareRead++
	return f.shares, nil
}
func (f *fakeStore) AddShare(_ context.Context, taskID, userID, userName string) error {
	f.addCalls++
	if f.addErr != nil {
		return f.addErr
	}
	f.shares = append(f.shares, store.Share{UserID: userID, UserName: userName})
	return nil
}

type fakeUsers struct {
	user   User
	err    error
	emails []string
}

func (f *fakeUsers) ByEmail(_ context.Context, email string) (User, error) {
	f.emails = append(f.emails, email)
	if f.err != nil {
		return User{}, f.err
	}
	return f.user, nil
}

func code(t *testing.T, err error) string {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *share.Error, got %v", err)
	}
	return e.Code
}

func owned() model.Task {
	return model.Task{ID: "task-1", UserID: "user-a", Title: "Ship it"}
}

// 1. Grants the share and records the display name; the e-mail is normalised.
func TestShareGrantsAndRecordsName(t *testing.T) {
	st := &fakeStore{task: owned()}
	users := &fakeUsers{user: User{ID: "user-b", Name: "Bia", Email: "bia@example.com"}}
	svc := New(st, users)

	resp, err := svc.Share(context.Background(), "user-a", "task-1", "  Bia@Example.COM ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected ok")
	}
	if got := users.emails; len(got) != 1 || got[0] != "bia@example.com" {
		t.Fatalf("email not normalised: %v", got)
	}
	if len(resp.SharedWith) != 1 || resp.SharedWith[0] != "user-b" {
		t.Fatalf("sharedWith = %v", resp.SharedWith)
	}
	if len(resp.SharedNames) != 1 || resp.SharedNames[0] != "Bia" {
		t.Fatalf("sharedNames = %v", resp.SharedNames)
	}
	if st.addCalls != 1 || st.shares[0].UserName != "Bia" {
		t.Fatalf("display name not stored: %+v", st.shares)
	}
}

// 2. Works when the row has no collaborators yet (no pre-existing ACL rows).
func TestShareWorksWithEmptyShareList(t *testing.T) {
	st := &fakeStore{task: owned(), shares: nil}
	users := &fakeUsers{user: User{ID: "user-b", Name: "Bia"}}
	svc := New(st, users)

	resp, err := svc.Share(context.Background(), "user-a", "task-1", "bia@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.SharedWith) != 1 {
		t.Fatalf("expected the new collaborator only, got %v", resp.SharedWith)
	}
}

// 3. Rejects an anonymous caller before touching anything else.
func TestShareRejectsAnonymous(t *testing.T) {
	st := &fakeStore{task: owned()}
	users := &fakeUsers{user: User{ID: "user-b"}}
	svc := New(st, users)

	_, err := svc.Share(context.Background(), "  ", "task-1", "bia@example.com")
	if got := code(t, err); got != CodeUnauthorized {
		t.Fatalf("code = %s", got)
	}
	if st.getCalls != 0 || st.addCalls != 0 {
		t.Fatal("store must not be touched for an anonymous caller")
	}
}

// 4. Rejects an invalid e-mail without a DB call.
func TestShareRejectsInvalidEmailWithoutDB(t *testing.T) {
	for _, email := range []string{"", "  ", "not-an-email", "a@b", "two words@example.com", "x@", "@example.com"} {
		st := &fakeStore{task: owned()}
		users := &fakeUsers{user: User{ID: "user-b"}}
		svc := New(st, users)

		_, err := svc.Share(context.Background(), "user-a", "task-1", email)
		if got := code(t, err); got != CodeInvalid {
			t.Fatalf("email %q: code = %s", email, got)
		}
		if st.getCalls != 0 || st.shareRead != 0 || st.addCalls != 0 || len(users.emails) != 0 {
			t.Fatalf("email %q: invalid input must not reach storage", email)
		}
	}
}

// 5. Rejects a non-owner.
func TestShareRejectsNonOwner(t *testing.T) {
	st := &fakeStore{task: owned()}
	users := &fakeUsers{user: User{ID: "user-c"}}
	svc := New(st, users)

	_, err := svc.Share(context.Background(), "user-b", "task-1", "bia@example.com")
	if got := code(t, err); got != CodeForbidden {
		t.Fatalf("code = %s", got)
	}
	if st.addCalls != 0 {
		t.Fatal("must not write for a non-owner")
	}
}

// 6. Maps a missing row to not_found.
func TestShareMissingTask(t *testing.T) {
	st := &fakeStore{taskErr: store.ErrNotFound}
	users := &fakeUsers{user: User{ID: "user-b"}}
	svc := New(st, users)

	_, err := svc.Share(context.Background(), "user-a", "nope", "bia@example.com")
	if got := code(t, err); got != CodeNotFound {
		t.Fatalf("code = %s", got)
	}
}

// 7. Reports no_user, self and already.
func TestShareErrorCodes(t *testing.T) {
	t.Run("no_user", func(t *testing.T) {
		st := &fakeStore{task: owned()}
		svc := New(st, &fakeUsers{err: ErrNoUser})
		_, err := svc.Share(context.Background(), "user-a", "task-1", "ghost@example.com")
		if got := code(t, err); got != CodeNoUser {
			t.Fatalf("code = %s", got)
		}
	})

	t.Run("self", func(t *testing.T) {
		st := &fakeStore{task: owned()}
		svc := New(st, &fakeUsers{user: User{ID: "user-a"}})
		_, err := svc.Share(context.Background(), "user-a", "task-1", "me@example.com")
		if got := code(t, err); got != CodeSelf {
			t.Fatalf("code = %s", got)
		}
	})

	t.Run("already", func(t *testing.T) {
		st := &fakeStore{task: owned(), shares: []store.Share{{UserID: "user-b", UserName: "Bia"}}}
		svc := New(st, &fakeUsers{user: User{ID: "user-b", Name: "Bia"}})
		_, err := svc.Share(context.Background(), "user-a", "task-1", "bia@example.com")
		if got := code(t, err); got != CodeAlready {
			t.Fatalf("code = %s", got)
		}
		if st.addCalls != 0 {
			t.Fatal("duplicate must not write")
		}
	})
}
