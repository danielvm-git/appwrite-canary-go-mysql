// Package store is the MySQL persistence layer plus the ports the API and the
// share service depend on.
package store

import (
	"context"
	"errors"

	"openruntimes/handler/pkg/model"
)

var (
	ErrNotFound  = errors.New("store: not found")
	ErrDuplicate = errors.New("store: duplicate")
)

type Share struct {
	UserID   string
	UserName string
}

// TaskFilter is the accessible-tasks query from the requirements:
// owner OR collaborator, optionally narrowed to a list and a search term.
//
//	ScopeMine   — rows I own
//	ScopeShared — rows shared with me
//	<list id>   — rows I own inside that list
type TaskFilter struct {
	UserID string
	Scope  string
	Search string
	Limit  int
}

const (
	ScopeMine   = "mine"
	ScopeShared = "shared"
)

type TaskStore interface {
	ListTasks(ctx context.Context, f TaskFilter) ([]model.Task, error)
	GetTask(ctx context.Context, id string) (model.Task, error)
	CreateTask(ctx context.Context, id, ownerID, ownerName string, in model.TaskInput) (model.Task, error)
	UpdateTask(ctx context.Context, id string, patch model.TaskPatch) (model.Task, error)
	DeleteTask(ctx context.Context, id string) error
	Shares(ctx context.Context, taskID string) ([]Share, error)
	AddShare(ctx context.Context, taskID, userID, userName string) error
}

type ListStore interface {
	ListLists(ctx context.Context, userID string) ([]model.List, error)
	CreateList(ctx context.Context, id, userID string, in model.ListInput) (model.List, error)
	DeleteList(ctx context.Context, id, userID string) error
}
