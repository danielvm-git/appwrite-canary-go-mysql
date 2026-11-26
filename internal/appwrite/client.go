// Package appwrite is the thin slice of the Appwrite server API this project
// needs: resolving an e-mail into a user id for share-by-email.
//
// Inside a Function the dynamic key arrives in the `x-appwrite-key` request
// header and the endpoint/project arrive as APPWRITE_FUNCTION_* variables, so a
// client is built per invocation.
package appwrite

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/share"
)

type Client struct {
	Endpoint string
	Project  string
	APIKey   string
	HTTP     *http.Client
}

func New(endpoint, project, key string) *Client {
	return &Client{
		Endpoint: strings.TrimSuffix(endpoint, "/"),
		Project:  project,
		APIKey:   key,
		HTTP:     &http.Client{Timeout: 10 * time.Second},
	}
}

type usersPage struct {
	Total int `json:"total"`
	Users []struct {
		ID    string `json:"$id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"users"`
}

// ByEmail returns the single user with that e-mail or share.ErrNoUser.
func (c *Client) ByEmail(ctx context.Context, email string) (share.User, error) {
	if c.Endpoint == "" || c.Project == "" || c.APIKey == "" {
		return share.User{}, fmt.Errorf("appwrite: endpoint, project and api key are required to look up %s", email)
	}

	query := fmt.Sprintf(`equal("email", [%q])`, email)
	endpoint := fmt.Sprintf("%s/users?queries[]=%s&limit=1", c.Endpoint, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return share.User{}, fmt.Errorf("appwrite: build request: %w", err)
	}
	req.Header.Set("X-Appwrite-Project", c.Project)
	req.Header.Set("X-Appwrite-Key", c.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return share.User{}, fmt.Errorf("appwrite: users lookup: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return share.User{}, share.ErrNoUser
	case http.StatusUnauthorized, http.StatusForbidden:
		return share.User{}, fmt.Errorf("appwrite: users lookup not allowed (status %d) — check the function key scopes users.read", resp.StatusCode)
	default:
		return share.User{}, fmt.Errorf("appwrite: users lookup failed with status %d", resp.StatusCode)
	}

	var page usersPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return share.User{}, fmt.Errorf("appwrite: decode users: %w", err)
	}
	if len(page.Users) == 0 {
		return share.User{}, share.ErrNoUser
	}
	u := page.Users[0]
	return share.User{ID: u.ID, Name: u.Name, Email: u.Email}, nil
}
