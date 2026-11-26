// Package model holds the wire types. The JSON shape is deliberately the same
// one the shared shell UI already consumes from Appwrite TablesDB
// (see specs/product/TODO_APP_REQUIREMENTS.md), so `src/lib/tasks.ts` can be
// pointed at this API without touching components.
package model

const (
	StatusOpen   = "open"
	StatusDoing  = "doing"
	StatusReview = "review"
	StatusDone   = "done"

	PriorityHigh = "high"
	PriorityMid  = "mid"
	PriorityLow  = "low"
)

var ValidStatuses = []string{StatusOpen, StatusDoing, StatusReview, StatusDone}
var ValidPriorities = []string{PriorityHigh, PriorityMid, PriorityLow}

type Task struct {
	ID           string   `json:"$id"`
	Sequence     int64    `json:"$sequence"`
	Title        string   `json:"title"`
	Notes        string   `json:"notes,omitempty"`
	Status       string   `json:"status"`
	Priority     string   `json:"priority,omitempty"`
	DueDate      string   `json:"dueDate,omitempty"`
	ListID       string   `json:"listId,omitempty"`
	UserID       string   `json:"userId"`
	OwnerName    string   `json:"ownerName,omitempty"`
	SharedWith   []string `json:"sharedWith"`
	SharedNames  []string `json:"sharedNames"`
	AttachmentID string   `json:"attachmentId,omitempty"`
	CreatedAt    string   `json:"$createdAt"`
	UpdatedAt    string   `json:"$updatedAt"`
}

type List struct {
	ID        string `json:"$id"`
	Sequence  int64  `json:"$sequence"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	UserID    string `json:"userId"`
	CreatedAt string `json:"$createdAt"`
	UpdatedAt string `json:"$updatedAt"`
}

// TaskInput is the create body. Owner fields are never taken from the client.
type TaskInput struct {
	Title        string `json:"title"`
	Notes        string `json:"notes"`
	Status       string `json:"status"`
	Priority     string `json:"priority"`
	DueDate      string `json:"dueDate"`
	ListID       string `json:"listId"`
	OwnerName    string `json:"ownerName"`
	AttachmentID string `json:"attachmentId"`
}

// TaskPatch uses pointers so "field absent" and "field set to empty" differ.
type TaskPatch struct {
	Title        *string `json:"title"`
	Notes        *string `json:"notes"`
	Status       *string `json:"status"`
	Priority     *string `json:"priority"`
	DueDate      *string `json:"dueDate"`
	ListID       *string `json:"listId"`
	AttachmentID *string `json:"attachmentId"`
}

type ListInput struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type ShareRequest struct {
	TaskID string `json:"taskId"`
	Email  string `json:"email"`
}

type ShareResponse struct {
	OK          bool     `json:"ok"`
	SharedWith  []string `json:"sharedWith,omitempty"`
	SharedNames []string `json:"sharedNames,omitempty"`
}

type ErrorResponse struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
