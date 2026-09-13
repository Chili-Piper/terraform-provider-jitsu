package client

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestWorkspaceCreateDeletedSlugRecovery(t *testing.T) {
	db, databaseURL := cleanupTestDatabase(t)
	execCleanupSQL(t, db, `CREATE TABLE newjitsu."Workspace" (id text PRIMARY KEY, slug text UNIQUE, deleted boolean NOT NULL, "updatedAt" timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE newjitsu."UserProfile" (id text PRIMARY KEY, admin boolean);
CREATE TABLE newjitsu."WorkspaceAccess" ("workspaceId" text, "userId" text, role text);`)
	for _, tc := range []struct {
		name            string
		conflict        int
		message         string
		readStatus      int
		remoteDeleted   bool
		dbDeleted       bool
		remoteSlug      string
		noDatabase      bool
		role            string
		admin           bool
		identityID      string
		identityLogin   string
		unauthenticated bool
		success         bool
	}{
		{name: "legacy validation conflict", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", success: true},
		{name: "validation conflict", conflict: 400, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", success: true},
		{name: "unique constraint conflict", conflict: 500, message: "Unique constraint failed on the fields: (`slug`)", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", success: true},
		{name: "workspace access denied", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 403, dbDeleted: true},
		{name: "active workspace", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteSlug: "reusable"},
		{name: "concurrently restored workspace", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, remoteSlug: "reusable"},
		{name: "another workspace returned", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "another"},
		{name: "database not configured", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", noDatabase: true},
		{name: "unrelated server failure", conflict: 500, message: "database unavailable", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable"},
		{name: "analyst cannot release slug", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", role: "analyst"},
		{name: "editor cannot release slug", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", role: "editor"},
		{name: "foreign owner cannot release slug", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", identityID: "foreign-owner"},
		{name: "administrator can release slug", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", role: "none", admin: true, success: true},
		{name: "administrator with analyst membership denied", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", role: "analyst", admin: true},
		{name: "administrator with editor membership denied", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", role: "editor", admin: true},
		{name: "legacy null owner can release slug", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", role: "legacy", success: true},
		{name: "service administrator can release slug", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", role: "none", identityID: "admin-service-account@jitsu.com", identityLogin: "admin/token", success: true},
		{name: "service identity requires provider match", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", role: "none", identityID: "admin-service-account@jitsu.com", identityLogin: "credentials"},
		{name: "unauthenticated identity", conflict: 500, message: "Invalid workspace slug: Slug is already taken", readStatus: 200, remoteDeleted: true, dbDeleted: true, remoteSlug: "reusable", unauthenticated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			execCleanupSQL(t, db, `TRUNCATE newjitsu."Workspace",newjitsu."WorkspaceAccess",newjitsu."UserProfile"`)
			execCleanupSQL(t, db, `INSERT INTO newjitsu."Workspace" (id,slug,deleted) VALUES ('old','reusable',$1),('other','untouched',true)`, tc.dbDeleted)
			execCleanupSQL(t, db, `INSERT INTO newjitsu."UserProfile" (id,admin) VALUES ('authenticated-user',$1),('foreign-owner',false)`, tc.admin)
			execCleanupSQL(t, db, `INSERT INTO newjitsu."WorkspaceAccess" ("workspaceId","userId",role) VALUES ('other','foreign-owner','owner')`)
			role := tc.role
			if role == "" {
				role = "owner"
			}
			if role != "none" {
				var databaseRole interface{} = role
				if role == "legacy" {
					databaseRole = nil
				}
				execCleanupSQL(t, db, `INSERT INTO newjitsu."WorkspaceAccess" ("workspaceId","userId",role) VALUES ('old','authenticated-user',$1)`, databaseRole)
			}
			identityID := tc.identityID
			if identityID == "" {
				identityID = "authenticated-user"
			}
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					http.Error(w, "missing authorization", http.StatusUnauthorized)
					return
				}
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/api/workspace":
					if posts.Add(1) == 1 {
						http.Error(w, tc.message, tc.conflict)
						return
					}
					if _, err := db.Exec(`INSERT INTO newjitsu."Workspace" (id,slug,deleted) VALUES ('new','reusable',false)`); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					json.NewEncoder(w).Encode(map[string]string{"id": "new"})
				case r.Method == http.MethodGet && r.URL.Path == "/api/workspace/reusable":
					w.WriteHeader(tc.readStatus)
					json.NewEncoder(w).Encode(map[string]interface{}{"id": "old", "slug": tc.remoteSlug, "deleted": tc.remoteDeleted})
				case r.Method == http.MethodGet && r.URL.Path == "/api/me":
					json.NewEncoder(w).Encode(map[string]interface{}{"auth": !tc.unauthenticated, "user": map[string]string{"internalId": identityID, "loginProvider": tc.identityLogin}})
				default:
					http.Error(w, "unexpected request", http.StatusBadRequest)
				}
			}))
			defer server.Close()
			connection := databaseURL
			if tc.noDatabase {
				connection = ""
			}
			c := New(server.URL, "test-token", connection, "test")
			defer c.Close()
			id, err := c.WorkspaceCreate(context.Background(), "Workspace", "reusable")
			if (err == nil) != tc.success {
				t.Fatalf("unexpected create result: id=%q err=%v", id, err)
			}
			var slug sql.NullString
			var deleted bool
			if err := db.QueryRow(`SELECT slug,deleted FROM newjitsu."Workspace" WHERE id='old'`).Scan(&slug, &deleted); err != nil {
				t.Fatal(err)
			}
			if deleted != tc.dbDeleted {
				t.Fatal("recovery changed the old workspace lifecycle state")
			}
			if tc.success {
				if id != "new" || posts.Load() != 2 || slug.Valid {
					t.Fatalf("expected a fresh workspace and released old slug: id=%q posts=%d slug=%v", id, posts.Load(), slug)
				}
			} else if posts.Load() != 1 || !slug.Valid || slug.String != "reusable" {
				t.Fatalf("refused recovery modified the old workspace or retried: posts=%d slug=%v", posts.Load(), slug)
			}
			var otherSlug string
			if err := db.QueryRow(`SELECT slug FROM newjitsu."Workspace" WHERE id='other'`).Scan(&otherSlug); err != nil {
				t.Fatal(err)
			}
			if otherSlug != "untouched" {
				t.Fatal("recovery modified an unrelated workspace")
			}
		})
	}
}
