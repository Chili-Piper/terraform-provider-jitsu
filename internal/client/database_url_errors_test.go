package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDatabaseURLErrorsDoNotExposeCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/ws/config/stream":
			http.Error(w, "Unique constraint failed on the fields: (`id`)", http.StatusInternalServerError)
		case r.Method == http.MethodPost && r.URL.Path == "/api/workspace":
			http.Error(w, "Invalid workspace slug: reusable is already taken", http.StatusBadRequest)
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspace/reusable":
			json.NewEncoder(w).Encode(map[string]interface{}{"id": "old", "slug": "reusable", "deleted": true})
		case r.Method == http.MethodGet && r.URL.Path == "/api/me":
			json.NewEncoder(w).Encode(map[string]interface{}{"auth": true, "user": map[string]string{"internalId": "user", "loginProvider": "credentials"}})
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	for name, databaseURL := range map[string]string{
		"url with unescaped percent":  "postgres://user:canary%zz@db.invalid:5432/jitsu",
		"keyword with unquoted space": "host=db.invalid user=user password=credential canary dbname=jitsu",
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			for path, call := range map[string]func(*Client) error{
				"soft-delete cleanup": func(c *Client) error {
					_, err := c.Create(ctx, "ws", "stream", map[string]interface{}{"id": "stream"})
					return err
				},
				"workspace slug release": func(c *Client) error {
					_, err := c.WorkspaceCreate(ctx, "Workspace", "reusable")
					return err
				},
			} {
				c := New(server.URL, "test", databaseURL, "audit")
				err := call(c)
				c.Close()
				if err == nil {
					t.Fatalf("%s: expected an invalid database_url error", path)
				}
				if strings.Contains(err.Error(), "canary") {
					t.Errorf("%s: database_url credential was exposed in the error: %v", path, err)
				}
				if !strings.Contains(err.Error(), "database_url is not a valid PostgreSQL connection string") {
					t.Errorf("%s: missing invalid database_url error: %v", path, err)
				}
			}
		})
	}
}
