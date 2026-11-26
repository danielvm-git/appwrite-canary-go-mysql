package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/model"
)

// MySQL implements TaskStore and ListStore over the schema in migrations/001_init.sql.
type MySQL struct {
	db *sql.DB
}

func NewMySQL(db *sql.DB) *MySQL { return &MySQL{db: db} }

const taskColumns = `t.id, t.seq, t.title, COALESCE(t.notes,''), t.status, COALESCE(t.priority,''),
	DATE_FORMAT(t.due_date, '%Y-%m-%dT%H:%i:%sZ'), COALESCE(t.list_id,''), t.user_id,
	COALESCE(t.owner_name,''), COALESCE(t.attachment_id,''), t.created_at, t.updated_at`

func (s *MySQL) ListTasks(ctx context.Context, f TaskFilter) ([]model.Task, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 500
	}

	args := []any{}
	where := ""

	switch {
	case f.Scope == ScopeShared:
		where = "EXISTS (SELECT 1 FROM task_shares sh WHERE sh.task_id = t.id AND sh.user_id = ?)"
		args = append(args, f.UserID)
	case f.Scope != "" && f.Scope != ScopeMine:
		where = "t.user_id = ? AND t.list_id = ?"
		args = append(args, f.UserID, f.Scope)
	default:
		where = "t.user_id = ?"
		args = append(args, f.UserID)
	}

	if q := strings.TrimSpace(f.Search); q != "" {
		where += " AND MATCH(t.title, t.notes) AGAINST (? IN BOOLEAN MODE)"
		args = append(args, booleanQuery(q))
	}

	query := fmt.Sprintf(`SELECT %s FROM tasks t WHERE %s ORDER BY t.seq DESC LIMIT %d`, taskColumns, where, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list tasks: %w", err)
	}
	defer rows.Close()

	tasks, err := scanTasks(rows)
	if err != nil {
		return nil, err
	}
	if err := s.hydrateShares(ctx, tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *MySQL) GetTask(ctx context.Context, id string) (model.Task, error) {
	query := fmt.Sprintf(`SELECT %s FROM tasks t WHERE t.id = ?`, taskColumns)
	rows, err := s.db.QueryContext(ctx, query, id)
	if err != nil {
		return model.Task{}, fmt.Errorf("store: get task: %w", err)
	}
	defer rows.Close()

	tasks, err := scanTasks(rows)
	if err != nil {
		return model.Task{}, err
	}
	if len(tasks) == 0 {
		return model.Task{}, ErrNotFound
	}
	if err := s.hydrateShares(ctx, tasks); err != nil {
		return model.Task{}, err
	}
	return tasks[0], nil
}

func (s *MySQL) CreateTask(ctx context.Context, id, ownerID, ownerName string, in model.TaskInput) (model.Task, error) {
	if in.Status == "" {
		in.Status = model.StatusOpen
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tasks (id, title, notes, status, priority, due_date, list_id, user_id, owner_name, attachment_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		in.Title,
		nullable(in.Notes),
		in.Status,
		nullable(in.Priority),
		nullableDate(in.DueDate),
		nullable(in.ListID),
		ownerID,
		nullable(ownerName),
		nullable(in.AttachmentID),
	)
	if err != nil {
		if isDuplicate(err) {
			return model.Task{}, ErrDuplicate
		}
		return model.Task{}, fmt.Errorf("store: create task: %w", err)
	}
	return s.GetTask(ctx, id)
}

func (s *MySQL) UpdateTask(ctx context.Context, id string, patch model.TaskPatch) (model.Task, error) {
	set := []string{}
	args := []any{}

	add := func(column string, value any) {
		set = append(set, column+" = ?")
		args = append(args, value)
	}
	if patch.Title != nil {
		add("title", *patch.Title)
	}
	if patch.Notes != nil {
		add("notes", nullable(*patch.Notes))
	}
	if patch.Status != nil {
		add("status", *patch.Status)
	}
	if patch.Priority != nil {
		add("priority", nullable(*patch.Priority))
	}
	if patch.DueDate != nil {
		add("due_date", nullableDate(*patch.DueDate))
	}
	if patch.ListID != nil {
		add("list_id", nullable(*patch.ListID))
	}
	if patch.AttachmentID != nil {
		add("attachment_id", nullable(*patch.AttachmentID))
	}
	if len(set) == 0 {
		return s.GetTask(ctx, id)
	}

	args = append(args, id)
	res, err := s.db.ExecContext(ctx, "UPDATE tasks SET "+strings.Join(set, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return model.Task{}, fmt.Errorf("store: update task: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetTask(ctx, id); err != nil {
			return model.Task{}, err
		}
	}
	return s.GetTask(ctx, id)
}

func (s *MySQL) DeleteTask(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete task: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MySQL) Shares(ctx context.Context, taskID string) ([]Share, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, COALESCE(user_name,'') FROM task_shares WHERE task_id = ? ORDER BY created_at`, taskID)
	if err != nil {
		return nil, fmt.Errorf("store: shares: %w", err)
	}
	defer rows.Close()

	out := []Share{}
	for rows.Next() {
		var sh Share
		if err := rows.Scan(&sh.UserID, &sh.UserName); err != nil {
			return nil, fmt.Errorf("store: scan share: %w", err)
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

func (s *MySQL) AddShare(ctx context.Context, taskID, userID, userName string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO task_shares (task_id, user_id, user_name) VALUES (?, ?, ?)`,
		taskID, userID, nullable(userName))
	if err != nil {
		if isDuplicate(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: add share: %w", err)
	}
	return nil
}

func (s *MySQL) ListLists(ctx context.Context, userID string) ([]model.List, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, seq, name, color, user_id, created_at, updated_at
		FROM lists WHERE user_id = ? ORDER BY seq DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list lists: %w", err)
	}
	defer rows.Close()

	out := []model.List{}
	for rows.Next() {
		var l model.List
		var created, updated time.Time
		if err := rows.Scan(&l.ID, &l.Sequence, &l.Name, &l.Color, &l.UserID, &created, &updated); err != nil {
			return nil, fmt.Errorf("store: scan list: %w", err)
		}
		l.CreatedAt = format(created)
		l.UpdatedAt = format(updated)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *MySQL) CreateList(ctx context.Context, id, userID string, in model.ListInput) (model.List, error) {
	color := in.Color
	if color == "" {
		color = "violet"
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO lists (id, name, color, user_id) VALUES (?, ?, ?, ?)`,
		id, in.Name, color, userID); err != nil {
		if isDuplicate(err) {
			return model.List{}, ErrDuplicate
		}
		return model.List{}, fmt.Errorf("store: create list: %w", err)
	}
	lists, err := s.ListLists(ctx, userID)
	if err != nil {
		return model.List{}, err
	}
	for _, l := range lists {
		if l.ID == id {
			return l, nil
		}
	}
	return model.List{}, ErrNotFound
}

func (s *MySQL) DeleteList(ctx context.Context, id, userID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM lists WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("store: delete list: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

func (s *MySQL) hydrateShares(ctx context.Context, tasks []model.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids := make([]any, 0, len(tasks))
	placeholders := make([]string, 0, len(tasks))
	for i := range tasks {
		ids = append(ids, tasks[i].ID)
		placeholders = append(placeholders, "?")
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT task_id, user_id, COALESCE(user_name,'') FROM task_shares
		 WHERE task_id IN (%s) ORDER BY created_at`, strings.Join(placeholders, ",")), ids...)
	if err != nil {
		return fmt.Errorf("store: hydrate shares: %w", err)
	}
	defer rows.Close()

	byTask := map[string][]Share{}
	for rows.Next() {
		var taskID string
		var sh Share
		if err := rows.Scan(&taskID, &sh.UserID, &sh.UserName); err != nil {
			return fmt.Errorf("store: scan share: %w", err)
		}
		byTask[taskID] = append(byTask[taskID], sh)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range tasks {
		for _, sh := range byTask[tasks[i].ID] {
			tasks[i].SharedWith = append(tasks[i].SharedWith, sh.UserID)
			tasks[i].SharedNames = append(tasks[i].SharedNames, sh.UserName)
		}
	}
	return nil
}

func scanTasks(rows *sql.Rows) ([]model.Task, error) {
	out := []model.Task{}
	for rows.Next() {
		var t model.Task
		var notes, priority, due, listID, ownerName, attachment string
		var created, updated time.Time
		if err := rows.Scan(&t.ID, &t.Sequence, &t.Title, &notes, &t.Status, &priority,
			&due, &listID, &t.UserID, &ownerName, &attachment, &created, &updated); err != nil {
			return nil, fmt.Errorf("store: scan task: %w", err)
		}
		t.Notes, t.Priority, t.DueDate, t.ListID = notes, priority, due, listID
		t.OwnerName, t.AttachmentID = ownerName, attachment
		t.CreatedAt, t.UpdatedAt = format(created), format(updated)
		out = append(out, t)
	}
	return out, rows.Err()
}

func format(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func nullable(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func nullableDate(v string) any {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02"} {
		if parsed, err := time.Parse(layout, v); err == nil {
			return parsed.UTC()
		}
	}
	return nil
}

// booleanQuery turns `foo bar` into `+foo* +bar*` so partial words still match.
func booleanQuery(q string) string {
	terms := strings.Fields(q)
	for i, term := range terms {
		term = strings.NewReplacer("+", "", "-", "", "*", "", "\"", "").Replace(term)
		terms[i] = "+" + term + "*"
	}
	return strings.Join(terms, " ")
}

func isDuplicate(err error) bool {
	var myErr *mysql.MySQLError
	return errors.As(err, &myErr) && myErr.Number == 1062
}
