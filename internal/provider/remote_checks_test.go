package provider_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	_ "github.com/lib/pq"
)

const (
	remoteCheckTimeout  = 20 * time.Second
	remoteCheckInterval = 500 * time.Millisecond
)

func testAccRemoteClient() *client.Client {
	consoleURL := testAccConsoleURL()
	databaseURL := testAccDatabaseURL()
	username := testAccUsername()
	password := testAccPassword()

	return client.New(consoleURL, username, password, databaseURL)
}

func testAccConsoleURL() string {
	consoleURL := os.Getenv("JITSU_CONSOLE_URL")
	if consoleURL == "" {
		consoleURL = "http://localhost:3300"
	}
	return consoleURL
}

func testAccDatabaseURL() string {
	databaseURL := os.Getenv("JITSU_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://reporting:plz_no_hack!@localhost:5432/reporting?sslmode=disable"
	}
	return databaseURL
}

func testAccUsername() string {
	username := os.Getenv("JITSU_USERNAME")
	if username == "" {
		username = "admin@jitsu.com"
	}
	return username
}

func testAccPassword() string {
	password := os.Getenv("JITSU_PASSWORD")
	if password == "" {
		password = "admin123"
	}
	return password
}

func testAccWithRetry(desc string, check func() error) error {
	deadline := time.Now().Add(remoteCheckTimeout)
	var lastErr error

	for {
		err := check()
		if err == nil {
			return nil
		}
		lastErr = err

		if time.Now().After(deadline) {
			return fmt.Errorf("%s failed after %s: %w", desc, remoteCheckTimeout, lastErr)
		}
		time.Sleep(remoteCheckInterval)
	}
}

func testAccGetResourceState(s *terraform.State, resourceName string) (*terraform.ResourceState, error) {
	rs, ok := s.RootModule().Resources[resourceName]
	if !ok {
		return nil, fmt.Errorf("resource %q not found in state", resourceName)
	}
	if rs.Primary == nil {
		return nil, fmt.Errorf("resource %q has no primary state", resourceName)
	}
	return rs, nil
}

func testAccRequiredAttr(rs *terraform.ResourceState, key string) (string, error) {
	v, ok := rs.Primary.Attributes[key]
	if !ok || v == "" {
		return "", fmt.Errorf("required state attribute %q is missing/empty", key)
	}
	return v, nil
}

func toStringSlice(v interface{}) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	raw, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("expected list value, got %T", v)
	}

	out := make([]string, 0, len(raw))
	for _, it := range raw {
		s, ok := it.(string)
		if !ok {
			return nil, fmt.Errorf("expected string list element, got %T", it)
		}
		out = append(out, s)
	}
	return out, nil
}

func sameStringElements(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	counts := map[string]int{}
	for _, v := range got {
		counts[v]++
	}
	for _, v := range want {
		counts[v]--
	}
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
}

func numberToInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}

func testAccCheckWorkspaceRemote(resourceName, expectedName, expectedSlug string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, err := testAccGetResourceState(s, resourceName)
		if err != nil {
			return err
		}
		workspaceID, err := testAccRequiredAttr(rs, "id")
		if err != nil {
			return err
		}

		c := testAccRemoteClient()
		defer c.Close()

		return testAccWithRetry("workspace remote check", func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result, err := c.WorkspaceRead(ctx, workspaceID)
			if err != nil {
				return err
			}
			if result == nil {
				return fmt.Errorf("workspace %q not found in API", workspaceID)
			}

			name, _ := result["name"].(string)
			if name != expectedName {
				return fmt.Errorf("workspace name mismatch: got %q want %q", name, expectedName)
			}

			db, err := sql.Open("postgres", testAccDatabaseURL())
			if err != nil {
				return fmt.Errorf("opening database connection: %w", err)
			}
			defer db.Close()

			var dbName string
			var dbSlug sql.NullString
			var dbDeleted bool
			err = db.QueryRowContext(
				ctx,
				`SELECT name, slug, deleted FROM newjitsu."Workspace" WHERE id = $1`,
				workspaceID,
			).Scan(&dbName, &dbSlug, &dbDeleted)
			if err != nil {
				return fmt.Errorf("querying workspace row: %w", err)
			}
			if dbDeleted {
				return fmt.Errorf("workspace %q is marked deleted in DB", workspaceID)
			}
			if dbName != expectedName {
				return fmt.Errorf("workspace DB name mismatch: got %q want %q", dbName, expectedName)
			}
			if !dbSlug.Valid || dbSlug.String != expectedSlug {
				return fmt.Errorf("workspace DB slug mismatch: got %q want %q", dbSlug.String, expectedSlug)
			}
			return nil
		})
	}
}

func testAccCheckFunctionRemote(resourceName, expectedName, expectedCode string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, err := testAccGetResourceState(s, resourceName)
		if err != nil {
			return err
		}
		workspaceID, err := testAccRequiredAttr(rs, "workspace_id")
		if err != nil {
			return err
		}
		id, err := testAccRequiredAttr(rs, "id")
		if err != nil {
			return err
		}

		c := testAccRemoteClient()
		defer c.Close()

		return testAccWithRetry("function remote check", func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result, err := c.Read(ctx, workspaceID, "function", id)
			if err != nil {
				return err
			}
			if result == nil {
				return fmt.Errorf("function %q/%q not found in API", workspaceID, id)
			}

			name, _ := result["name"].(string)
			if name != expectedName {
				return fmt.Errorf("function name mismatch: got %q want %q", name, expectedName)
			}
			code, _ := result["code"].(string)
			if code != expectedCode {
				return fmt.Errorf("function code mismatch")
			}
			return nil
		})
	}
}

func testAccCheckDestinationRemote(
	resourceName, expectedName, expectedType, expectedProtocol string,
	expectedHosts []string,
	expectedUsername, expectedDatabase string,
) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, err := testAccGetResourceState(s, resourceName)
		if err != nil {
			return err
		}
		workspaceID, err := testAccRequiredAttr(rs, "workspace_id")
		if err != nil {
			return err
		}
		id, err := testAccRequiredAttr(rs, "id")
		if err != nil {
			return err
		}

		c := testAccRemoteClient()
		defer c.Close()

		return testAccWithRetry("destination remote check", func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result, err := c.Read(ctx, workspaceID, "destination", id)
			if err != nil {
				return err
			}
			if result == nil {
				return fmt.Errorf("destination %q/%q not found in API", workspaceID, id)
			}

			name, _ := result["name"].(string)
			if name != expectedName {
				return fmt.Errorf("destination name mismatch: got %q want %q", name, expectedName)
			}
			destType, _ := result["destinationType"].(string)
			if destType != expectedType {
				return fmt.Errorf("destination type mismatch: got %q want %q", destType, expectedType)
			}
			protocol, _ := result["protocol"].(string)
			if protocol != expectedProtocol {
				return fmt.Errorf("destination protocol mismatch: got %q want %q", protocol, expectedProtocol)
			}
			username, _ := result["username"].(string)
			if username != expectedUsername {
				return fmt.Errorf("destination username mismatch: got %q want %q", username, expectedUsername)
			}
			database, _ := result["database"].(string)
			if database != expectedDatabase {
				return fmt.Errorf("destination database mismatch: got %q want %q", database, expectedDatabase)
			}

			hosts, err := toStringSlice(result["hosts"])
			if err != nil {
				return err
			}
			if !sameStringElements(hosts, expectedHosts) {
				return fmt.Errorf("destination hosts mismatch: got %v want %v", hosts, expectedHosts)
			}
			return nil
		})
	}
}

func hasKeyWithID(v interface{}, keyID string) (bool, error) {
	raw, ok := v.([]interface{})
	if !ok {
		return false, fmt.Errorf("expected key list, got %T", v)
	}
	for _, it := range raw {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		if id == keyID {
			return true, nil
		}
	}
	return false, nil
}

func testAccCheckStreamRemote(resourceName, expectedName, expectedPublicKeyID string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, err := testAccGetResourceState(s, resourceName)
		if err != nil {
			return err
		}
		workspaceID, err := testAccRequiredAttr(rs, "workspace_id")
		if err != nil {
			return err
		}
		id, err := testAccRequiredAttr(rs, "id")
		if err != nil {
			return err
		}

		c := testAccRemoteClient()
		defer c.Close()

		return testAccWithRetry("stream remote check", func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result, err := c.Read(ctx, workspaceID, "stream", id)
			if err != nil {
				return err
			}
			if result == nil {
				return fmt.Errorf("stream %q/%q not found in API", workspaceID, id)
			}

			name, _ := result["name"].(string)
			if name != expectedName {
				return fmt.Errorf("stream name mismatch: got %q want %q", name, expectedName)
			}

			publicKeysRaw, ok := result["publicKeys"]
			if !ok {
				return fmt.Errorf("stream %q missing publicKeys in API response", id)
			}
			found, err := hasKeyWithID(publicKeysRaw, expectedPublicKeyID)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("stream %q does not contain public key id %q", id, expectedPublicKeyID)
			}
			return nil
		})
	}
}

func testAccCheckLinkRemote(
	resourceName, expectedMode, expectedDataLayout string,
	expectedFrequency, expectedBatchSize int64,
) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, err := testAccGetResourceState(s, resourceName)
		if err != nil {
			return err
		}
		workspaceID, err := testAccRequiredAttr(rs, "workspace_id")
		if err != nil {
			return err
		}
		fromID, err := testAccRequiredAttr(rs, "from_id")
		if err != nil {
			return err
		}
		toID, err := testAccRequiredAttr(rs, "to_id")
		if err != nil {
			return err
		}

		c := testAccRemoteClient()
		defer c.Close()

		return testAccWithRetry("link remote check", func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			links, err := c.List(ctx, workspaceID, "link")
			if err != nil {
				return err
			}

			var found map[string]interface{}
			for _, link := range links {
				f, _ := link["fromId"].(string)
				t, _ := link["toId"].(string)
				deleted, _ := link["deleted"].(bool)
				if f == fromID && t == toID && !deleted {
					found = link
					break
				}
			}
			if found == nil {
				return fmt.Errorf("active link from %q to %q not found in workspace %q", fromID, toID, workspaceID)
			}

			data, _ := found["data"].(map[string]interface{})
			if data == nil {
				return fmt.Errorf("link data is missing")
			}

			mode, _ := data["mode"].(string)
			if mode != expectedMode {
				return fmt.Errorf("link mode mismatch: got %q want %q", mode, expectedMode)
			}
			layout, _ := data["dataLayout"].(string)
			if layout != expectedDataLayout {
				return fmt.Errorf("link dataLayout mismatch: got %q want %q", layout, expectedDataLayout)
			}
			frequency, ok := numberToInt64(data["frequency"])
			if !ok || frequency != expectedFrequency {
				return fmt.Errorf("link frequency mismatch: got %v want %d", data["frequency"], expectedFrequency)
			}
			batchSize, ok := numberToInt64(data["batchSize"])
			if !ok || batchSize != expectedBatchSize {
				return fmt.Errorf("link batchSize mismatch: got %v want %d", data["batchSize"], expectedBatchSize)
			}
			return nil
		})
	}
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "returned 404") ||
		strings.Contains(msg, "status\":404") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "does not exist")
}

func testAccCheckWorkspaceDeletedInDB(ctx context.Context, db *sql.DB, workspaceID string) error {
	var deleted sql.NullBool
	err := db.QueryRowContext(
		ctx,
		`SELECT deleted FROM newjitsu."Workspace" WHERE id = $1`,
		workspaceID,
	).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !deleted.Valid || !deleted.Bool {
		return fmt.Errorf("workspace %q still active in DB (deleted=%v)", workspaceID, deleted)
	}
	return nil
}

func testAccCheckConfigObjectDeletedInDB(ctx context.Context, db *sql.DB, id string) error {
	var deleted sql.NullBool
	err := db.QueryRowContext(
		ctx,
		`SELECT deleted FROM newjitsu."ConfigurationObject" WHERE id = $1`,
		id,
	).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !deleted.Valid || !deleted.Bool {
		return fmt.Errorf("config object %q still active in DB (deleted=%v)", id, deleted)
	}
	return nil
}

func testAccCheckLinkDeletedInDB(ctx context.Context, db *sql.DB, linkID string) error {
	var deleted sql.NullBool
	err := db.QueryRowContext(
		ctx,
		`SELECT deleted FROM newjitsu."ConfigurationObjectLink" WHERE id = $1`,
		linkID,
	).Scan(&deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !deleted.Valid || !deleted.Bool {
		return fmt.Errorf("link %q still active in DB (deleted=%v)", linkID, deleted)
	}
	return nil
}

func testAccCheckDestroyRemote(s *terraform.State) error {
	c := testAccRemoteClient()
	defer c.Close()

	db, err := sql.Open("postgres", testAccDatabaseURL())
	if err != nil {
		return fmt.Errorf("opening database connection for destroy checks: %w", err)
	}
	defer db.Close()

	return testAccWithRetry("destroy remote check", func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		for resourceName, rs := range s.RootModule().Resources {
			if rs == nil || rs.Primary == nil {
				continue
			}
			if !strings.HasPrefix(rs.Type, "jitsu_") {
				continue
			}

			switch rs.Type {
			case "jitsu_workspace":
				workspaceID, err := testAccRequiredAttr(rs, "id")
				if err != nil {
					return fmt.Errorf("%s: %w", resourceName, err)
				}

				result, err := c.WorkspaceRead(ctx, workspaceID)
				if err != nil && !isNotFoundError(err) {
					return fmt.Errorf("%s: reading workspace from API: %w", resourceName, err)
				}
				if err == nil && result != nil {
					return fmt.Errorf("%s: workspace %q still exists in API after destroy", resourceName, workspaceID)
				}

				if err := testAccCheckWorkspaceDeletedInDB(ctx, db, workspaceID); err != nil {
					return fmt.Errorf("%s: %w", resourceName, err)
				}

			case "jitsu_function", "jitsu_destination", "jitsu_stream":
				workspaceID, err := testAccRequiredAttr(rs, "workspace_id")
				if err != nil {
					return fmt.Errorf("%s: %w", resourceName, err)
				}
				id, err := testAccRequiredAttr(rs, "id")
				if err != nil {
					return fmt.Errorf("%s: %w", resourceName, err)
				}

				resourceType := strings.TrimPrefix(rs.Type, "jitsu_")
				result, err := c.Read(ctx, workspaceID, resourceType, id)
				if err != nil && !isNotFoundError(err) {
					return fmt.Errorf("%s: reading %s from API: %w", resourceName, resourceType, err)
				}
				if err == nil && result != nil {
					return fmt.Errorf("%s: %s %q still exists in API after destroy", resourceName, resourceType, id)
				}

				if err := testAccCheckConfigObjectDeletedInDB(ctx, db, id); err != nil {
					return fmt.Errorf("%s: %w", resourceName, err)
				}

			case "jitsu_link":
				workspaceID, err := testAccRequiredAttr(rs, "workspace_id")
				if err != nil {
					return fmt.Errorf("%s: %w", resourceName, err)
				}
				linkID, err := testAccRequiredAttr(rs, "id")
				if err != nil {
					return fmt.Errorf("%s: %w", resourceName, err)
				}
				fromID, err := testAccRequiredAttr(rs, "from_id")
				if err != nil {
					return fmt.Errorf("%s: %w", resourceName, err)
				}
				toID, err := testAccRequiredAttr(rs, "to_id")
				if err != nil {
					return fmt.Errorf("%s: %w", resourceName, err)
				}

				links, err := c.List(ctx, workspaceID, "link")
				if err != nil && !isNotFoundError(err) {
					return fmt.Errorf("%s: listing links from API: %w", resourceName, err)
				}
				if err == nil {
					for _, link := range links {
						id, _ := link["id"].(string)
						f, _ := link["fromId"].(string)
						t, _ := link["toId"].(string)
						deleted, _ := link["deleted"].(bool)

						matchByID := id == linkID
						matchByEdge := f == fromID && t == toID
						if (matchByID || matchByEdge) && !deleted {
							return fmt.Errorf("%s: link %q (%s -> %s) still active in API after destroy", resourceName, linkID, fromID, toID)
						}
					}
				}

				if err := testAccCheckLinkDeletedInDB(ctx, db, linkID); err != nil {
					return fmt.Errorf("%s: %w", resourceName, err)
				}
			}
		}

		return nil
	})
}
