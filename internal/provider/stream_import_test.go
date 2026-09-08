package provider_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

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
