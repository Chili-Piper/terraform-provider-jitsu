package provider_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestDestination_typeValidation(t *testing.T) {
	config := func(destinationType string) string {
		return fmt.Sprintf(`
%s
resource "jitsu_destination" "test" {
  workspace_id     = "type-validation"
  id               = "destination"
  name             = "Destination"
  destination_type = %q

  clickhouse = {
    hosts = ["clickhouse:8123"]
  }
}
`, testAccProviderConfig(t), destinationType)
	}
	steps := []resource.TestStep{}
	for _, destinationType := range []string{"clickhouse", "devnull", "salesforce"} {
		steps = append(steps, resource.TestStep{Config: config(destinationType), PlanOnly: true, ExpectNonEmptyPlan: true})
	}
	for _, destinationType := range []string{"snowflake", "mysql", "ClickHouse", ""} {
		steps = append(steps, resource.TestStep{
			Config:      config(destinationType),
			PlanOnly:    true,
			ExpectError: regexp.MustCompile("Invalid destination type"),
		})
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    steps,
	})
}

func TestDestination_importRejectsUnsupportedType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if req.Method != http.MethodGet || req.URL.Path != "/api/workspace/config/destination/warehouse" {
			http.NotFound(w, req)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "warehouse", "workspaceId": "workspace", "type": "destination",
			"name": "Warehouse", "destinationType": "snowflake", "account": "acme",
		})
	}))
	defer server.Close()

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
provider "jitsu" {
  console_url = %q
  auth_token  = "test-token"
}
resource "jitsu_destination" "test" {
  workspace_id     = "workspace"
  id               = "warehouse"
  name             = "Warehouse"
  destination_type = "clickhouse"

  clickhouse = {
    hosts = ["clickhouse:8123"]
  }
}
`, server.URL),
			ResourceName:  "jitsu_destination.test",
			ImportState:   true,
			ImportStateId: "workspace/warehouse",
			ExpectError:   regexp.MustCompile("Unsupported destination type"),
		}},
	})
}
