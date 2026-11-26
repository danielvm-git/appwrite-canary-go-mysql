// Package share implements "share a task with a user identified by e-mail",
// the per-canary slice the requirements single out as the language proof.
//
// The decision order and the error codes are contractual (see
// specs/product/TODO_APP_REQUIREMENTS.md): unauthorized, invalid, not_found,
// forbidden, no_user, self, already.
package share

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/model"
	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/store"
)

// Codes returned to the client.
const (
	CodeUnauthorized = "unauthorized"
	CodeInvalid      = "invalid"
	CodeNotFound     = "not_found"
	CodeForbidden    = "forbidden"
	CodeNoUser       = "no_user"
	CodeSelf         = "self"
	CodeAlready      = "already"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func newError(code, message string) *Error { return &Error{Code: code, Message: message} }

type User struct {
	ID    string
	Name  string
	Email string
}

var ErrNoUser = errors.New("share: no user with that email")

// Users resolves an e-mail into a user. Implemented by the Appwrite Users API.
type Users interface {
	ByEmail(ctx context.Context, email string) (User, error)
}

type Service struct {
	tasks store.TaskStore
	users Users
}

func New(tasks store.TaskStore, users Users) *Service {
	return &Service{tasks: tasks, users: users}
}

// Share appends the invitee as a collaborator on the task and returns the
// materialised sharedWith/sharedNames arrays the UI reads.
func (s *Service) Share(ctx context.Context, actorID, taskID, rawEmail string) (model.ShareResponse, error) {
	if strings.TrimSpace(actorID) == "" {
		return model.ShareResponse{}, newError(CodeUnauthorized, "missing caller user id")
	}

	email, ok := normalizeEmail(rawEmail)
	if !ok {
		return model.ShareResponse{}, newError(CodeInvalid, "invalid email")
	}

	task, err := s.tasks.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return model.ShareResponse{}, newError(CodeNotFound, "task not found")
		}
		return model.ShareResponse{}, err
	}

	if task.UserID != actorID {
		return model.ShareResponse{}, newError(CodeForbidden, "only the owner can share this task")
	}

	invitee, err := s.users.ByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNoUser) {
			return model.ShareResponse{}, newError(CodeNoUser, "no Appwrite user for "+email)
		}
		return model.ShareResponse{}, err
	}

	if invitee.ID == actorID {
		return model.ShareResponse{}, newError(CodeSelf, "cannot share a task with yourself")
	}

	existing, err := s.tasks.Shares(ctx, taskID)
	if err != nil {
		return model.ShareResponse{}, err
	}
	for _, sh := range existing {
		if sh.UserID == invitee.ID {
			return model.ShareResponse{}, newError(CodeAlready, "already shared with "+email)
		}
	}

	if err := s.tasks.AddShare(ctx, taskID, invitee.ID, invitee.Name); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			return model.ShareResponse{}, newError(CodeAlready, "already shared with "+email)
		}
		return model.ShareResponse{}, err
	}

	resp := model.ShareResponse{OK: true, SharedWith: []string{}, SharedNames: []string{}}
	for _, sh := range existing {
		resp.SharedWith = append(resp.SharedWith, sh.UserID)
		resp.SharedNames = append(resp.SharedNames, sh.UserName)
	}
	resp.SharedWith = append(resp.SharedWith, invitee.ID)
	resp.SharedNames = append(resp.SharedNames, invitee.Name)
	return resp, nil
}

func normalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > 254 || strings.ContainsAny(email, " <>\t\r\n") {
		return "", false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", false
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return "", false
	}
	domain := email[at+1:]
	// Require a dotted domain: `a@b` parses fine per RFC 5322 but is never a
	// reachable Appwrite account.
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", false
	}
	return email, true
}
