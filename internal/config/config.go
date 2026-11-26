// Package config reads the function's environment in one place.
//
// The values come from Appwrite Function variables (Console → Function →
// Settings → Variables) or from a local .env when running `make run`.
package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// HTTP
	Bind string
	Port string

	// MySQL / HeatWave
	DBHost      string
	DBPort      string
	DBName      string
	DBUser      string
	DBPassword  string
	DBTLSCA     string // PEM contents of the endpoint CA
	DBTLSCAPath string // path to the same PEM, used when DBTLSCA is empty

	// Appwrite (used only to resolve an e-mail into a user id for share-by-email)
	AppwriteEndpoint  string
	AppwriteProjectID string
	AppwriteAPIKey    string

	// Dev affordance: accept x-dev-user instead of x-appwrite-user-id.
	AllowDevUser bool

	// Version / migrations
	Version      string
	MigrateToken string
}

func Load() Config {
	c := Config{
		Bind:              os.Getenv("BIND"), // empty = all interfaces (required inside an Appwrite Function)
		Port:              env("PORT", "3000"),
		DBHost:            env("MYSQL_HOST", "10.0.1.126"),
		DBPort:            env("MYSQL_PORT", "3306"),
		DBName:            env("MYSQL_DB", "todo_mysql"),
		DBUser:            env("MYSQL_USER", "todo"),
		DBPassword:        env("MYSQL_PASSWORD", ""),
		DBTLSCA:           os.Getenv("MYSQL_CA_PEM"),
		DBTLSCAPath:       os.Getenv("MYSQL_CA_PATH"),
		AppwriteEndpoint:  firstEnv("APPWRITE_FUNCTION_API_ENDPOINT", "APPWRITE_ENDPOINT"),
		AppwriteProjectID: firstEnv("APPWRITE_FUNCTION_PROJECT_ID", "APPWRITE_PROJECT_ID"),
		AppwriteAPIKey:    os.Getenv("APPWRITE_API_KEY"),
		AllowDevUser:      env("ALLOW_DEV_USER", "false") == "true",
		Version:           env("APP_VERSION", "dev"),
		MigrateToken:      os.Getenv("MIGRATE_TOKEN"),
	}
	if c.AppwriteProjectID == "" {
		c.AppwriteProjectID = env("MYSQL_PROJECT_FALLBACK", "")
	}
	return c
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// IntEnv is a small helper for optional numeric settings.
func IntEnv(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
