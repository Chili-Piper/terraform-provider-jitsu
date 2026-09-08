package provider_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestDestination_clearSettings(t *testing.T) {
	for _, imported := range []bool{false, true} {
		name := "remove configured settings"
		if imported {
			name = "preserve imported password"
		}
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var remote map[string]interface{}
			if imported {
				remote = map[string]interface{}{
					"id": "destination", "workspaceId": "workspace", "name": "Before", "type": "destination",
					"destinationType": "clickhouse", "hosts": []interface{}{"localhost:8123"},
					"protocol": "clickhouse-secure", "username": "default", "database": "default", "password": "test-secret",
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if !strings.HasPrefix(req.URL.Path, "/api/workspace/config/destination") {
					http.NotFound(w, req)
					return
				}
				switch req.Method {
				case http.MethodPost, http.MethodPut:
					var patch map[string]interface{}
					if err := json.NewDecoder(req.Body).Decode(&patch); err != nil {
						http.Error(w, err.Error(), 400)
						return
					}
					if req.Method == http.MethodPost {
						remote = map[string]interface{}{}
					}
					for key, value := range patch {
						if value == "__*$undef$*__" {
							delete(remote, key)
						} else {
							remote[key] = value
						}
					}
					for key, value := range map[string]interface{}{"protocol": "clickhouse-secure", "username": "default", "database": "default"} {
						if _, ok := remote[key]; !ok {
							remote[key] = value
						}
					}
					json.NewEncoder(w).Encode(map[string]string{"id": "destination"})
				case http.MethodGet:
					if remote == nil {
						http.NotFound(w, req)
						return
					}
					masked := make(map[string]interface{}, len(remote))
					for key, value := range remote {
						masked[key] = value
					}
					masked["password"] = "********"
					json.NewEncoder(w).Encode(masked)
				case http.MethodDelete:
					remote = nil
					json.NewEncoder(w).Encode(map[string]bool{"success": true})
				default:
					http.Error(w, "unexpected method", 405)
				}
			}))
			defer server.Close()
			config := func(name, settings string) string {
				return fmt.Sprintf(`
provider "jitsu" {
 console_url = %q
 auth_token = "test-token"
}
resource "jitsu_destination" "test" {
 workspace_id = "workspace"
 id = "destination"
 name = %q
 destination_type = "clickhouse"
 clickhouse = {
  hosts = ["localhost:8123"]
  %s
 }
}
`, server.URL, name, settings)
			}
			initial := `protocol = "http"
username = "custom"
database = "other"
password = "test-secret"
cluster = "old_cluster"`
			if imported {
				initial = `password = "test-secret"`
			}
			check := func(_ *terraform.State) error {
				mu.Lock()
				defer mu.Unlock()
				expected := map[string]interface{}{"protocol": "clickhouse-secure", "username": "default", "database": "default", "password": "", "cluster": ""}
				if imported {
					expected["password"] = "test-secret"
				}
				for key, want := range expected {
					if remote[key] != want {
						return fmt.Errorf("%s was not reset or preserved as expected", key)
					}
				}
				return nil
			}
			steps := []resource.TestStep{{Config: config("Before", initial)}}
			if imported {
				steps = []resource.TestStep{{Config: config("Before", ""), ResourceName: "jitsu_destination.test", ImportState: true, ImportStateId: "workspace/destination", ImportStatePersist: true}}
			}
			steps = append(steps,
				resource.TestStep{Config: config("After", ""), Check: resource.ComposeAggregateTestCheckFunc(
					check,
					resource.TestCheckResourceAttr("jitsu_destination.test", "clickhouse.protocol", "clickhouse-secure"),
					resource.TestCheckResourceAttr("jitsu_destination.test", "clickhouse.username", "default"),
					resource.TestCheckResourceAttr("jitsu_destination.test", "clickhouse.database", "default"),
					resource.TestCheckResourceAttr("jitsu_destination.test", "clickhouse.cluster", ""),
					resource.TestCheckNoResourceAttr("jitsu_destination.test", "clickhouse.password"),
				)},
				resource.TestStep{Config: config("After", ""), PlanOnly: true},
			)
			resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: testAccProtoV6ProviderFactories, Steps: steps})
		})
	}
}
