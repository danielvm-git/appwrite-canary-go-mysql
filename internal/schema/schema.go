// Package schema embeds the SQL migrations so the deployed function is the only
// thing that can change the database.
//
// Deploys are CI-only (see .github/workflows/deploy.yml): after the new function
// version is active, CI calls POST /admin/migrate with MIGRATE_TOKEN. No SSH, no
// local mysql client, no manual step.
package schema

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed sql/*.sql
var files embed.FS

type Result struct {
	Files      []string `json:"files"`
	Statements int      `json:"statements"`
}

// Migrations lists the embedded files in order.
func Migrations() ([]string, error) {
	entries, err := files.ReadDir("sql")
	if err != nil {
		return nil, fmt.Errorf("schema: read embed: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Apply runs every embedded migration. Every statement is idempotent
// (CREATE TABLE IF NOT EXISTS), so applying twice is safe.
func Apply(ctx context.Context, db *sql.DB) (Result, error) {
	names, err := Migrations()
	if err != nil {
		return Result{}, err
	}

	res := Result{Files: names}
	for _, name := range names {
		raw, err := files.ReadFile("sql/" + name)
		if err != nil {
			return res, fmt.Errorf("schema: read %s: %w", name, err)
		}
		for _, stmt := range splitStatements(string(raw)) {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return res, fmt.Errorf("schema: %s: %w", name, err)
			}
			res.Statements++
		}
	}
	return res, nil
}

// splitStatements splits on semicolons that end a line, dropping comments and
// blanks. The migrations are plain DDL, so this is enough and stays readable.
func splitStatements(source string) []string {
	out := []string{}
	var current strings.Builder
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		current.WriteString(line)
		current.WriteString("\n")
		if strings.HasSuffix(trimmed, ";") {
			stmt := strings.TrimSpace(current.String())
			stmt = strings.TrimSuffix(stmt, ";")
			if stmt != "" {
				out = append(out, stmt)
			}
			current.Reset()
		}
	}
	if stmt := strings.TrimSpace(current.String()); stmt != "" {
		out = append(out, stmt)
	}
	return out
}
