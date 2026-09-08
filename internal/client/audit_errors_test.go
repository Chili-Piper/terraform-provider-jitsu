package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuditErrorsDoNotExposeResponseSecrets(t *testing.T) {
	for _, body := range []string{
		`{"error":"validation failed","input":{"password":"credential-canary","keyFile":"credential-canary"}}`,
		"upstream rejected bearer credential-canary",
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, body, http.StatusBadRequest) }))
		c := New(server.URL, "test", "", "audit")
		ctx := context.Background()
		calls := map[string]func() error{
			"create": func() error {
				_, err := c.Create(ctx, "ws", "destination", map[string]interface{}{"password": "credential-canary"})
				return err
			},
			"read":             func() error { _, err := c.Read(ctx, "ws", "destination", "id"); return err },
			"update":           func() error { _, err := c.Update(ctx, "ws", "destination", "id", nil); return err },
			"delete":           func() error { return c.Delete(ctx, "ws", "destination", "id") },
			"list":             func() error { _, err := c.List(ctx, "ws", "link"); return err },
			"delete link":      func() error { return c.DeleteLink(ctx, "ws", "id") },
			"workspace create": func() error { _, err := c.WorkspaceCreate(ctx, "name", "slug"); return err },
			"workspace read":   func() error { _, err := c.WorkspaceRead(ctx, "ws"); return err },
			"workspace update": func() error { _, err := c.WorkspaceUpdate(ctx, "ws", "name", "slug"); return err },
			"workspace delete": func() error { return c.WorkspaceDelete(ctx, "ws") },
		}
		for name, call := range calls {
			t.Run(name, func(t *testing.T) {
				err := call()
				if err == nil {
					t.Fatal("expected HTTP failure")
				}
				if strings.Contains(err.Error(), "credential-canary") {
					t.Error("API response credential was exposed in the error")
				}
				if !strings.Contains(err.Error(), "400") {
					t.Error("missing HTTP status")
				}
			})
		}
		server.Close()
	}
}
