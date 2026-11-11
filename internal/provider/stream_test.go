package provider_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccStream_basic(t *testing.T) {
	suffix := testAccSuffix()
	streamID := "site_test_acc_" + suffix
	keyID := "js.test-acc-browser-" + suffix

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyRemote,
		Steps: []resource.TestStep{
			// Create with keys and Read
			{
				Config: testAccStreamConfig(t, suffix, streamID, keyID, "Test Stream"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_stream.test", "name", "Test Stream"),
					resource.TestCheckResourceAttr("jitsu_stream.test", "id", streamID),
					testAccCheckStreamRemote("jitsu_stream.test", "Test Stream", keyID),
				),
			},
			// Update name
			{
				Config: testAccStreamConfig(t, suffix, streamID, keyID, "Updated Stream"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_stream.test", "name", "Updated Stream"),
					testAccCheckStreamRemote("jitsu_stream.test", "Updated Stream", keyID),
				),
			},
			// Import (keys ignored — API returns hashed values)
			{
				ResourceName: "jitsu_stream.test",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					wsID := s.RootModule().Resources["jitsu_workspace.test"].Primary.ID
					return wsID + "/" + streamID, nil
				},
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"public_keys", "private_keys"},
			},
		},
	})
}

func testAccStreamConfig(t *testing.T, suffix, streamID, keyID, name string) string {
	providerConfig := testAccProviderConfig(t)
	return fmt.Sprintf(`
%s

resource "jitsu_workspace" "test" {
  name = %q
  slug = %q
}

resource "jitsu_stream" "test" {
  workspace_id = jitsu_workspace.test.id
  id           = %q
  name         = %q

  public_keys = [{
    id        = %q
    plaintext = %q
  }]
}
`, providerConfig, testAccWorkspaceName("TF Stream Workspace", suffix), testAccWorkspaceSlug("tf-acc-stream", suffix), streamID, name, keyID, keyID)
}

func TestAccStream_importPreservesKeys(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	ctx := context.Background()
	c := testAccRemoteClient()
	defer c.Close()
	suffix := testAccSuffix()
	workspaceID, err := c.WorkspaceCreate(ctx, "Stream import test", "stream-import-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	defer c.WorkspaceDelete(ctx, workspaceID)
	streamID := "stream_import_" + suffix
	payload := map[string]interface{}{"id": streamID, "workspaceId": workspaceID, "type": "stream", "name": "Before"}
	if _, err := c.Create(ctx, workspaceID, "stream", payload); err != nil {
		t.Fatal(err)
	}
	defer c.Delete(ctx, workspaceID, "stream", streamID)
	payload["publicKeys"] = []map[string]string{{"id": "public_" + suffix, "plaintext": "public-secret-" + suffix}}
	payload["privateKeys"] = []map[string]string{{"id": "private_" + suffix, "plaintext": "private-secret-" + suffix}}
	if _, err := c.Update(ctx, workspaceID, "stream", streamID, payload); err != nil {
		t.Fatal(err)
	}
	config := func(name, keys string) string {
		return fmt.Sprintf(`
%s
resource "jitsu_stream" "test" {
 workspace_id = %q
 id = %q
 name = %q
 %s
}
`, testAccProviderConfig(t), workspaceID, streamID, name, keys)
	}
	check := func(empty bool) resource.TestCheckFunc {
		return func(_ *terraform.State) error {
			result, err := c.Read(ctx, workspaceID, "stream", streamID)
			if err != nil {
				return err
			}
			for field, id := range map[string]string{"publicKeys": "public_" + suffix, "privateKeys": "private_" + suffix} {
				keys, ok := result[field].([]interface{})
				if !ok {
					return fmt.Errorf("missing %s", field)
				}
				if empty {
					if len(keys) != 0 {
						return fmt.Errorf("%s were not revoked", field)
					}
					continue
				}
				if len(keys) != 1 || keys[0].(map[string]interface{})["id"] != id {
					return fmt.Errorf("%s changed during rename", field)
				}
			}
			return nil
		}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config("Before", ""), ResourceName: "jitsu_stream.test", ImportState: true, ImportStateId: workspaceID + "/" + streamID, ImportStatePersist: true},
			{Config: config("After", ""), Check: check(false)},
			{Config: config("After", ""), PlanOnly: true},
			{Config: config("After", "public_keys = []\nprivate_keys = []"), Check: check(true)},
			{Config: config("After", "public_keys = []\nprivate_keys = []"), PlanOnly: true},
		},
	})
}

func TestStream_importPreservesKeys(t *testing.T) {
	for _, tc := range []struct{ name, clear string }{
		{name: "remove configured lists"},
		{name: "explicit empty lists", clear: "public_keys = []\nprivate_keys = []"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			remote := map[string]interface{}{
				"id": "stream", "name": "Before", "workspaceId": "workspace", "type": "stream",
				"publicKeys":  []interface{}{map[string]interface{}{"id": "original_public"}},
				"privateKeys": []interface{}{map[string]interface{}{"id": "original_private"}},
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if req.URL.Path != "/api/workspace/config/stream/stream" {
					http.NotFound(w, req)
					return
				}
				switch req.Method {
				case http.MethodGet:
					if remote == nil {
						http.NotFound(w, req)
						return
					}
					json.NewEncoder(w).Encode(remote)
				case http.MethodPut:
					var patch map[string]interface{}
					if err := json.NewDecoder(req.Body).Decode(&patch); err != nil {
						http.Error(w, err.Error(), 400)
						return
					}
					for key, value := range patch {
						if key == "publicKeys" || key == "privateKeys" {
							keys, ok := value.([]interface{})
							if !ok {
								http.Error(w, "invalid keys", 400)
								return
							}
							for _, v := range keys {
								delete(v.(map[string]interface{}), "plaintext")
							}
						}
						remote[key] = value
					}
					json.NewEncoder(w).Encode(map[string]bool{"success": true})
				case http.MethodDelete:
					remote = nil
					json.NewEncoder(w).Encode(map[string]bool{"success": true})
				default:
					http.Error(w, "unexpected method", 405)
				}
			}))
			defer server.Close()
			config := func(name, keys string) string {
				return fmt.Sprintf(`
provider "jitsu" {
 console_url = %q
 auth_token = "test-token"
}
resource "jitsu_stream" "test" {
 workspace_id = "workspace"
 id = "stream"
 name = %q
 %s
}
`, server.URL, name, keys)
			}
			checkKeys := func(public, private string) resource.TestCheckFunc {
				return func(_ *terraform.State) error {
					mu.Lock()
					defer mu.Unlock()
					for key, want := range map[string]string{"publicKeys": public, "privateKeys": private} {
						keys := remote[key].([]interface{})
						if want == "" {
							if len(keys) != 0 {
								return fmt.Errorf("%s were not revoked", key)
							}
							continue
						}
						if len(keys) != 1 || keys[0].(map[string]interface{})["id"] != want {
							return fmt.Errorf("%s changed unexpectedly: %v", key, keys)
						}
					}
					return nil
				}
			}
			managed := `public_keys = [{id = "managed_public", plaintext = "public-secret"}]
private_keys = [{id = "managed_private", plaintext = "private-secret"}]`
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{Config: config("Before", ""), ResourceName: "jitsu_stream.test", ImportState: true, ImportStateId: "workspace/stream", ImportStatePersist: true},
					{Config: config("Renamed", ""), Check: checkKeys("original_public", "original_private")},
					{Config: config("Renamed", ""), PlanOnly: true},
					{Config: config("Renamed", managed), Check: checkKeys("managed_public", "managed_private")},
					{Config: config("Renamed", tc.clear), Check: checkKeys("", "")},
					{Config: config("Renamed", tc.clear), PlanOnly: true},
				},
			})
		})
	}
}

func TestAccStream_repairsKeyDrift(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	ctx := context.Background()
	c := testAccRemoteClient()
	defer c.Close()
	suffix := testAccSuffix()
	ws, err := c.WorkspaceCreate(ctx, "Key drift test", "key-drift-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	defer c.WorkspaceDelete(ctx, ws)
	id := "key_drift_" + suffix
	config := fmt.Sprintf(`
%s
resource "jitsu_stream" "test" {
 workspace_id = %q
 id = %q
 name = "Key drift test"
 public_keys = [{id = "managed_public", plaintext = "public-secret"}]
 private_keys = [{id = "managed_private", plaintext = "private-secret"}]
}
`, testAccProviderConfig(t), ws, id)
	drift := func(add bool) func() {
		return func() {
			payload := map[string]interface{}{}
			for field, managed := range map[string]string{"publicKeys": "managed_public", "privateKeys": "managed_private"} {
				keys := []map[string]string{}
				if add {
					keys = append(keys, map[string]string{"id": managed, "plaintext": "managed-secret"}, map[string]string{"id": "external", "plaintext": "external-secret"})
				}
				payload[field] = keys
			}
			if _, err := c.Update(ctx, ws, "stream", id, payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	check := func(_ *terraform.State) error {
		remote, err := c.Read(ctx, ws, "stream", id)
		if err != nil {
			return err
		}
		for field, managed := range map[string]string{"publicKeys": "managed_public", "privateKeys": "managed_private"} {
			keys, ok := remote[field].([]interface{})
			if !ok || len(keys) != 1 || keys[0].(map[string]interface{})["id"] != managed {
				return fmt.Errorf("%s drift was not repaired", field)
			}
		}
		return nil
	}
	update := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("jitsu_stream.test", plancheck.ResourceActionUpdate)}}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config, Check: check},
			{Config: config, PreConfig: drift(false), ConfigPlanChecks: update, Check: check},
			{Config: config, PlanOnly: true},
			{Config: config, PreConfig: drift(true), ConfigPlanChecks: update, Check: check},
			{Config: config, PlanOnly: true},
		},
	})
}

func TestAccStream_rejectsDuplicateKeyIDs(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	for _, field := range []string{"public_keys", "private_keys"} {
		t.Run(field, func(t *testing.T) {
			ctx := context.Background()
			c := testAccRemoteClient()
			defer c.Close()
			suffix := testAccSuffix()
			ws, err := c.WorkspaceCreate(ctx, "Duplicate key ID test", "duplicate-key-"+suffix)
			if err != nil {
				t.Fatal(err)
			}
			defer c.WorkspaceDelete(ctx, ws)
			id := "duplicate_key_" + suffix
			defer c.Delete(ctx, ws, "stream", id)
			otherField := "private_keys"
			if field == otherField {
				otherField = "public_keys"
			}
			config := func(keys string) string {
				return fmt.Sprintf(`
%s
resource "jitsu_stream" "test" {
 workspace_id = %q
 id = %q
 name = "Duplicate key ID test"
 %s = %s
 %s = [{id = "first", plaintext = "other-secret"}]
}
`, testAccProviderConfig(t), ws, id, field, keys, otherField)
			}
			valid := config(`[{id = "first", plaintext = "first-secret"}, {id = "second", plaintext = "second-secret"}]`)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{Config: config(`[{id = "duplicate", plaintext = "first-secret"}, {id = "duplicate", plaintext = "second-secret"}]`), ExpectError: regexp.MustCompile("key IDs must be unique")},
					{Config: valid, PreConfig: func() {
						remote, err := c.Read(ctx, ws, "stream", id)
						if err != nil {
							t.Fatal(err)
						}
						if remote != nil {
							t.Fatal("invalid key IDs created a remote stream")
						}
					}},
					{Config: valid, PlanOnly: true},
				},
			})
		})
	}
}

func TestAccStream_rejectsEmptyKeyIDs(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	for _, field := range []string{"public_keys", "private_keys"} {
		t.Run(field, func(t *testing.T) {
			ctx := context.Background()
			c := testAccRemoteClient()
			defer c.Close()
			suffix := testAccSuffix()
			ws, err := c.WorkspaceCreate(ctx, "Empty key ID test", "empty-key-"+suffix)
			if err != nil {
				t.Fatal(err)
			}
			defer c.WorkspaceDelete(ctx, ws)
			id := "empty_key_" + suffix
			defer c.Delete(ctx, ws, "stream", id)
			otherField := "private_keys"
			if field == otherField {
				otherField = "public_keys"
			}
			config := func(keys string) string {
				return fmt.Sprintf(`
%s
resource "jitsu_stream" "test" {
 workspace_id = %q
 id = %q
 name = "Empty key ID test"
 %s = %s
 %s = [{id = "first", plaintext = "other-secret"}]
}
`, testAccProviderConfig(t), ws, id, field, keys, otherField)
			}
			valid := config(`[{id = "first", plaintext = "first-secret"}, {id = "second", plaintext = "second-secret"}]`)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{Config: config(`[{id = "", plaintext = "first-secret"}]`), ExpectError: regexp.MustCompile("key IDs must not be empty")},
					{Config: valid, PreConfig: func() {
						remote, err := c.Read(ctx, ws, "stream", id)
						if err != nil {
							t.Fatal(err)
						}
						if remote != nil {
							t.Fatal("invalid key IDs created a remote stream")
						}
					}},
					{Config: valid, PlanOnly: true},
				},
			})
		})
	}
}

func TestAccStream_repairsEmptyKeyID(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	for field, remoteField := range map[string]string{"public_keys": "publicKeys", "private_keys": "privateKeys"} {
		t.Run(field, func(t *testing.T) {
			ctx := context.Background()
			c := testAccRemoteClient()
			defer c.Close()
			suffix := testAccSuffix()
			ws, err := c.WorkspaceCreate(ctx, "Repair empty key ID", "repair-empty-key-"+suffix)
			if err != nil {
				t.Fatal(err)
			}
			defer c.WorkspaceDelete(ctx, ws)
			id := "repair_empty_key_" + suffix
			defer c.Delete(ctx, ws, "stream", id)
			config := fmt.Sprintf(`
%s
resource "jitsu_stream" "test" {
 workspace_id = %q
 id = %q
 name = "Repair empty key ID"
 %s = [{id = "managed", plaintext = "managed-secret"}]
}
`, testAccProviderConfig(t), ws, id, field)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{Config: config},
					{Config: config, PreConfig: func() {
						_, err := c.Update(ctx, ws, "stream", id, map[string]interface{}{
							remoteField: []map[string]string{{"id": "", "plaintext": "broken-secret"}},
						})
						if err != nil {
							t.Fatal(err)
						}
					}, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("jitsu_stream.test", plancheck.ResourceActionUpdate),
					}}, Check: func(_ *terraform.State) error {
						remote, err := c.Read(ctx, ws, "stream", id)
						if err != nil {
							return err
						}
						keys, ok := remote[remoteField].([]interface{})
						if !ok || len(keys) != 1 {
							return fmt.Errorf("expected one repaired key, got %v", remote[remoteField])
						}
						key := keys[0].(map[string]interface{})
						if key["id"] != "managed" || key["hint"] != "man*ret" {
							return fmt.Errorf("empty-ID key was not replaced: %v", key)
						}
						return nil
					}},
					{Config: config, PlanOnly: true},
				},
			})
		})
	}
}

func TestAccStream_rejectsEmptyKeyRotation(t *testing.T) {
	suffix := testAccSuffix()
	key := "old-secret-" + suffix
	config := testAccStreamConfig(t, suffix, "empty_key_"+suffix, key, "Key rotation")
	empty := strings.Replace(config, fmt.Sprintf("plaintext = %q", key), `plaintext = ""`, 1)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: testAccProtoV6ProviderFactories, CheckDestroy: testAccCheckDestroyRemote, Steps: []resource.TestStep{
		{Config: config},
		{Config: empty, ExpectError: regexp.MustCompile("plaintext must not be empty")},
		{Config: config, PlanOnly: true},
	}})
}

func TestAccStream_preservesOriginRestrictions(t *testing.T) {
	suffix := testAccSuffix()
	config := testAccStreamConfig(t, suffix, "origin_"+suffix, "key_"+suffix, "Original")
	c := testAccRemoteClient()
	defer c.Close()
	var ws, id string
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: testAccProtoV6ProviderFactories, CheckDestroy: testAccCheckDestroyRemote, Steps: []resource.TestStep{
		{Config: config, Check: func(s *terraform.State) error {
			rs := s.RootModule().Resources["jitsu_stream.test"].Primary
			ws, id = rs.Attributes["workspace_id"], rs.ID
			_, err := c.Update(context.Background(), ws, "stream", id, map[string]interface{}{"authorizedJavaScriptDomains": "https://trusted.example.com"})
			if err != nil {
				return err
			}
			remote, err := c.Read(context.Background(), ws, "stream", id)
			if err != nil {
				return err
			}
			if remote["authorizedJavaScriptDomains"] != "https://trusted.example.com" {
				return fmt.Errorf("fixture did not persist origin restriction: %v", remote["authorizedJavaScriptDomains"])
			}
			return nil
		}},
		{Config: strings.Replace(config, `name         = "Original"`, `name         = "Renamed"`, 1), Check: func(_ *terraform.State) error {
			remote, err := c.Read(context.Background(), ws, "stream", id)
			if err != nil {
				return err
			}
			if remote["authorizedJavaScriptDomains"] != "https://trusted.example.com" {
				return fmt.Errorf("rename removed browser-origin restriction: got %v", remote["authorizedJavaScriptDomains"])
			}
			return nil
		}},
		{Config: strings.Replace(config, `name         = "Original"`, `name         = "Renamed"`, 1), PlanOnly: true},
	}})
}
