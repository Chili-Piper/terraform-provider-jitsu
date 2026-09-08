package client

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestCreate_SoftDeleteRecovery(t *testing.T) {
	db, databaseURL := cleanupTestDatabase(t)
	for _, tc := range []struct {
		name, workspace, objectType string
		deleted, success            bool
		blockingLinkWorkspace       string
		blockingLinkDeleted         bool
	}{
		{name: "matching deleted object", workspace: "ours", objectType: "stream", deleted: true, success: true},
		{name: "another workspace", workspace: "theirs", objectType: "stream", deleted: true},
		{name: "another resource type", workspace: "ours", objectType: "destination", deleted: true},
		{name: "active object", workspace: "ours", objectType: "stream"},
		{name: "active link rolls back cleanup", workspace: "ours", objectType: "stream", deleted: true, blockingLinkWorkspace: "ours"},
		{name: "foreign deleted link is preserved", workspace: "ours", objectType: "stream", deleted: true, blockingLinkWorkspace: "theirs", blockingLinkDeleted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			execCleanupSQL(t, db, `TRUNCATE newjitsu."ConfigurationObjectLink", newjitsu."ConfigurationObject"`)
			execCleanupSQL(t, db, `INSERT INTO newjitsu."ConfigurationObject" (id,"workspaceId",type,deleted) VALUES ('target',$1,$2,$3),('peer','ours','destination',false)`, tc.workspace, tc.objectType, tc.deleted)
			execCleanupSQL(t, db, `INSERT INTO newjitsu."ConfigurationObjectLink" (id,"workspaceId",type,deleted,"fromId","toId") VALUES ('soft_link',$1,'push',true,'target','peer')`, tc.workspace)
			wantLinks := 1
			if tc.blockingLinkWorkspace != "" {
				execCleanupSQL(t, db, `INSERT INTO newjitsu."ConfigurationObjectLink" (id,"workspaceId",type,deleted,"fromId","toId") VALUES ('blocking_link',$1,'push',$2,'target','peer')`, tc.blockingLinkWorkspace, tc.blockingLinkDeleted)
				wantLinks++
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/ours/config/stream" {
					http.Error(w, "unexpected request", 400)
					return
				}
				if calls.Add(1) == 1 {
					http.Error(w, "Unique constraint failed", 500)
					return
				}
				if _, err := db.Exec(`INSERT INTO newjitsu."ConfigurationObject" (id,"workspaceId",type,deleted) VALUES ('target','ours','stream',false)`); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":"target"}`)
			}))
			defer server.Close()
			c := New(server.URL, "fake-token", databaseURL, "test")
			defer c.Close()
			_, err := c.Create(context.Background(), "ours", "stream", map[string]interface{}{"id": "target", "type": "stream"})
			if tc.success {
				if err != nil {
					t.Fatal(err)
				}
				if calls.Load() != 2 {
					t.Fatalf("expected a retry, got %d requests", calls.Load())
				}
				wantLinks = 0
			} else {
				if err == nil {
					t.Fatal("expected cleanup to refuse the conflicting object")
				}
				if calls.Load() != 1 {
					t.Fatalf("unexpected retry after refused cleanup: %d requests", calls.Load())
				}
			}
			var workspace, objectType string
			var deleted bool
			if err := db.QueryRow(`SELECT "workspaceId",type,deleted FROM newjitsu."ConfigurationObject" WHERE id='target'`).Scan(&workspace, &objectType, &deleted); err != nil {
				t.Fatal(err)
			}
			if tc.success {
				if workspace != "ours" || objectType != "stream" || deleted {
					t.Fatalf("object was not recreated: %s %s %v", workspace, objectType, deleted)
				}
			} else if workspace != tc.workspace || objectType != tc.objectType || deleted != tc.deleted {
				t.Fatalf("conflicting object changed: %s %s %v", workspace, objectType, deleted)
			}
			var links int
			if err := db.QueryRow(`SELECT count(*) FROM newjitsu."ConfigurationObjectLink"`).Scan(&links); err != nil {
				t.Fatal(err)
			}
			if links != wantLinks {
				t.Fatalf("expected %d preserved links, got %d", wantLinks, links)
			}
		})
	}
	for _, tc := range []struct {
		name, workspace, linkType string
		success                   bool
	}{
		{"matching deleted link", "ours", "push", true},
		{"another workspace link", "theirs", "push", false},
		{"another link type", "ours", "sync", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			execCleanupSQL(t, db, `TRUNCATE newjitsu."ConfigurationObjectLink", newjitsu."ConfigurationObject"`)
			execCleanupSQL(t, db, `INSERT INTO newjitsu."ConfigurationObject" (id,"workspaceId",type,deleted) VALUES ('source','ours','stream',false),('destination','ours','destination',false)`)
			execCleanupSQL(t, db, `INSERT INTO newjitsu."ConfigurationObjectLink" (id,"workspaceId",type,deleted,"fromId","toId") VALUES ('link',$1,$2,true,'source','destination')`, tc.workspace, tc.linkType)
			c := New("http://unused", "fake-token", databaseURL, "test")
			defer c.Close()
			err := c.hardDeleteSoftDeleted(context.Background(), "ours", "link", "ConfigurationObjectLink", "push")
			if (err == nil) != tc.success {
				t.Fatalf("unexpected cleanup result: %v", err)
			}
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM newjitsu."ConfigurationObjectLink"`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if (count == 0) != tc.success {
				t.Fatalf("unexpected remaining links: %d", count)
			}
		})
	}
}

func cleanupTestDatabase(t *testing.T) (*sql.DB, string) {
	t.Helper()
	for _, binary := range []string{"initdb", "pg_ctl"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("%s is required for isolated PostgreSQL recovery tests", binary)
		}
	}
	if os.Geteuid() == 0 {
		t.Skip("initdb requires a non-root user")
	}
	dir, err := os.MkdirTemp("", "jitsu-pg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	data := filepath.Join(dir, "data")
	run := func(name string, args ...string) {
		t.Helper()
		if output, err := exec.Command(name, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v\n%s", name, err, output)
		}
	}
	run("initdb", "-D", data, "-A", "trust", "-U", "jitsu_test", "--no-locale")
	run("pg_ctl", "-D", data, "-l", filepath.Join(dir, "postgres.log"), "-w", "start", "-o", fmt.Sprintf("-F -h '' -k %s", dir))
	t.Cleanup(func() {
		if output, err := exec.Command("pg_ctl", "-D", data, "-m", "immediate", "-w", "stop").CombinedOutput(); err != nil {
			t.Errorf("stopping test postgres: %v\n%s", err, output)
		}
	})
	databaseURL := fmt.Sprintf("host=%s user=jitsu_test dbname=postgres sslmode=disable", dir)
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	execCleanupSQL(t, db, `CREATE SCHEMA newjitsu;
CREATE TABLE newjitsu."ConfigurationObject" (id text PRIMARY KEY, "workspaceId" text NOT NULL, type text NOT NULL, deleted boolean);
CREATE TABLE newjitsu."ConfigurationObjectLink" (id text PRIMARY KEY, "workspaceId" text NOT NULL, type text, deleted boolean, "fromId" text REFERENCES newjitsu."ConfigurationObject"(id), "toId" text REFERENCES newjitsu."ConfigurationObject"(id));`)
	return db, databaseURL
}

func execCleanupSQL(t *testing.T, db *sql.DB, query string, args ...interface{}) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
