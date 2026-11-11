package provider_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccDestination_basic(t *testing.T) {
	suffix := testAccSuffix()
	destinationID := "dest_test_acc_" + suffix

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyRemote,
		Steps: []resource.TestStep{
			// Create and Read
			{
				Config: testAccDestinationConfig(t, suffix, destinationID, "Test Destination"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_destination.test", "name", "Test Destination"),
					resource.TestCheckResourceAttr("jitsu_destination.test", "destination_type", "clickhouse"),
					resource.TestCheckResourceAttr("jitsu_destination.test", "id", destinationID),
					resource.TestCheckResourceAttr("jitsu_destination.test", "clickhouse.protocol", "http"),
					resource.TestCheckResourceAttr("jitsu_destination.test", "clickhouse.username", "reporting"),
					resource.TestCheckResourceAttr("jitsu_destination.test", "clickhouse.database", "default"),
					testAccCheckDestinationRemote(
						"jitsu_destination.test",
						"Test Destination",
						"clickhouse",
						"http",
						[]string{"clickhouse:8123"},
						"reporting",
						"default",
					),
				),
			},
			// Update name
			{
				Config: testAccDestinationConfig(t, suffix, destinationID, "Updated Destination"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_destination.test", "name", "Updated Destination"),
					testAccCheckDestinationRemote(
						"jitsu_destination.test",
						"Updated Destination",
						"clickhouse",
						"http",
						[]string{"clickhouse:8123"},
						"reporting",
						"default",
					),
				),
			},
			// Import (password ignored — API returns masked value)
			{
				ResourceName: "jitsu_destination.test",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					wsID := s.RootModule().Resources["jitsu_workspace.test"].Primary.ID
					return wsID + "/" + destinationID, nil
				},
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"clickhouse.password"},
			},
		},
	})
}

func TestAccDestination_validationRequiresMatchingBlock(t *testing.T) {
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccDestinationValidationConfig(t, "bigquery", "", false),
				ExpectError: regexp.MustCompile(`BigQuery destinations must define the bigquery block\.`),
			},
			{
				Config:      testAccDestinationValidationConfig(t, "clickhouse", "", false),
				ExpectError: regexp.MustCompile(`"clickhouse" destinations must define the clickhouse block\.`),
			},
			{
				Config:      testAccDestinationValidationConfig(t, "bigquery", "clickhouse", false),
				ExpectError: regexp.MustCompile(`BigQuery destinations cannot define the clickhouse block\.`),
			},
			{
				Config:      testAccDestinationValidationConfig(t, "clickhouse", "bigquery", false),
				ExpectError: regexp.MustCompile(`"clickhouse" destinations cannot define the bigquery block\.`),
			},
			{
				Config:      testAccDestinationValidationConfig(t, "clickhouse", "both", false),
				ExpectError: regexp.MustCompile(`"clickhouse" destinations cannot define the bigquery block\.`),
			},
			{
				Config:      testAccDestinationValidationConfig(t, "postgres", "", false),
				ExpectError: regexp.MustCompile(`"postgres" destinations must define the postgres block\.`),
			},
			{
				Config:      testAccDestinationValidationConfig(t, "postgres", "clickhouse", false),
				ExpectError: regexp.MustCompile(`"postgres" destinations cannot define the clickhouse block\.`),
			},
			{
				Config:      testAccDestinationValidationConfig(t, "clickhouse", "postgres", false),
				ExpectError: regexp.MustCompile(`"clickhouse" destinations cannot define the postgres block\.`),
			},
		},
	})
}

func testAccDestinationConfig(t *testing.T, suffix, destinationID, name string) string {
	providerConfig := testAccProviderConfig(t)
	return fmt.Sprintf(`
%s

resource "jitsu_workspace" "test" {
  name = %q
  slug = %q
}

resource "jitsu_destination" "test" {
  workspace_id     = jitsu_workspace.test.id
  id               = %q
  name             = %q
  destination_type = "clickhouse"
  clickhouse = {
    protocol = "http"
    hosts    = ["clickhouse:8123"]
    username = "reporting"
    password = ""
    database = "default"
  }
}
`, providerConfig, testAccWorkspaceName("TF Destination Workspace", suffix), testAccWorkspaceSlug("tf-acc-dest", suffix), destinationID, name)
}

func testAccDestinationValidationConfig(t *testing.T, destinationType, block string, withWorkspace bool) string {
	t.Helper()

	providerConfig := testAccProviderConfig(t)
	workspaceID := `"workspace-id"`
	workspaceConfig := ""
	if withWorkspace {
		workspaceID = "jitsu_workspace.test.id"
		workspaceConfig = `
resource "jitsu_workspace" "test" {
  name = "Validation Workspace"
  slug = "validation-workspace"
}
`
	}

	resourceBody := ""
	switch block {
	case "clickhouse":
		resourceBody = `
  clickhouse = {
    hosts = ["clickhouse:8123"]
  }
`
	case "bigquery":
		resourceBody = `
  bigquery = {
    credentials = "{}"
    project_id  = "project-id"
    bq_dataset  = "dataset"
  }
`
	case "postgres":
		resourceBody = `
  postgres = {
    host     = "10.0.0.1"
    database = "events"
    username = "events"
  }
`
	case "both":
		resourceBody = `
  clickhouse = {
    hosts = ["clickhouse:8123"]
  }
  bigquery = {
    credentials = "{}"
    project_id  = "project-id"
    bq_dataset  = "dataset"
  }
`
	}

	return fmt.Sprintf(`
%s

%s
resource "jitsu_destination" "test" {
  workspace_id     = %s
  id               = "validation-destination"
  name             = "Validation Destination"
  destination_type = %q
%s
}
`, providerConfig, workspaceConfig, workspaceID, destinationType, resourceBody)
}

func TestAccDestination_switchToPasswordlessClickHouse(t *testing.T) {
	suffix := testAccSuffix()
	destinationID := "destination_type_" + suffix
	config := func(destinationType, settings string) string {
		return fmt.Sprintf(`
%s
resource "jitsu_workspace" "test" {
 name = %q
 slug = %q
}
resource "jitsu_destination" "test" {
 workspace_id = jitsu_workspace.test.id
 id = %q
 name = "Destination type change"
 destination_type = %q
 %s
}
`, testAccProviderConfig(t), "Destination type "+suffix, "destination-type-"+suffix, destinationID, destinationType, settings)
	}
	bigquery := config("bigquery", `bigquery = {
 credentials = "{}"
 project_id = "test-project"
 bq_dataset = "test_dataset"
}`)
	clickhouse := config("clickhouse", `clickhouse = {
 hosts = ["clickhouse:8123"]
 protocol = "http"
 username = "reporting"
}`)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyRemote,
		Steps: []resource.TestStep{
			{Config: bigquery},
			{Config: clickhouse, Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("jitsu_destination.test", "id", destinationID),
				resource.TestCheckResourceAttr("jitsu_destination.test", "destination_type", "clickhouse"),
				resource.TestCheckNoResourceAttr("jitsu_destination.test", "clickhouse.password"),
				func(s *terraform.State) error {
					rs, err := testAccGetResourceState(s, "jitsu_destination.test")
					if err != nil {
						return err
					}
					db, err := sql.Open("postgres", testAccDatabaseURL())
					if err != nil {
						return err
					}
					defer db.Close()
					var destinationType string
					var password sql.NullString
					if err := db.QueryRow(`SELECT config->>'destinationType', config->>'password' FROM newjitsu."ConfigurationObject" WHERE id=$1 AND "workspaceId"=$2 AND deleted=false`, destinationID, rs.Primary.Attributes["workspace_id"]).Scan(&destinationType, &password); err != nil {
						return err
					}
					if destinationType != "clickhouse" || !password.Valid || password.String != "" {
						return fmt.Errorf("destination was not changed to passwordless ClickHouse")
					}
					return nil
				},
			)},
			{Config: clickhouse, PlanOnly: true},
		},
	})
}

func TestAccDestination_withoutPassword(t *testing.T) {
	suffix := testAccSuffix()
	config := func(name string) string {
		return strings.Replace(testAccDestinationConfig(t, suffix, "passwordless_"+suffix, name), `password = ""`, "", 1)
	}
	check := resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckNoResourceAttr("jitsu_destination.test", "clickhouse.password"),
		func(s *terraform.State) error {
			rs, err := testAccGetResourceState(s, "jitsu_destination.test")
			if err != nil {
				return err
			}
			db, err := sql.Open("postgres", testAccDatabaseURL())
			if err != nil {
				return err
			}
			defer db.Close()
			var password string
			if err := db.QueryRow(`SELECT config->>'password' FROM newjitsu."ConfigurationObject" WHERE id=$1 AND "workspaceId"=$2 AND deleted=false`, rs.Primary.ID, rs.Primary.Attributes["workspace_id"]).Scan(&password); err != nil {
				return err
			}
			if password != "" {
				return fmt.Errorf("expected empty remote password")
			}
			return nil
		},
	)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: testAccProtoV6ProviderFactories, CheckDestroy: testAccCheckDestroyRemote, Steps: []resource.TestStep{
		{Config: config("Passwordless"), Check: check},
		{Config: config("Passwordless"), PlanOnly: true},
		{Config: config("Renamed passwordless"), Check: check},
		{Config: config("Renamed passwordless"), PlanOnly: true},
	}})
}

func TestAccDestination_clearSettings(t *testing.T) {
	suffix := testAccSuffix()
	destinationID := "destination_reset_" + suffix
	config := func(settings string) string {
		return fmt.Sprintf(`
%s
resource "jitsu_workspace" "test" {
 name = %q
 slug = %q
}
resource "jitsu_destination" "test" {
 workspace_id = jitsu_workspace.test.id
 id = %q
 name = "Reset settings"
 destination_type = "clickhouse"
 clickhouse = {
  hosts = ["clickhouse:8123"]
  %s
 }
}
`, testAccProviderConfig(t), "Destination reset "+suffix, "destination-reset-"+suffix, destinationID, settings)
	}
	initial := `protocol = "http"
username = "reporting"
database = "analytics"
password = "test-secret"
cluster = "old_cluster"`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyRemote,
		Steps: []resource.TestStep{
			{Config: config(initial)},
			{Config: config(""), Check: func(s *terraform.State) error {
				rs, err := testAccGetResourceState(s, "jitsu_destination.test")
				if err != nil {
					return err
				}
				db, err := sql.Open("postgres", testAccDatabaseURL())
				if err != nil {
					return err
				}
				defer db.Close()
				var protocol, username, database, password string
				var cluster string
				err = db.QueryRow(`SELECT config->>'protocol',config->>'username',config->>'database',config->>'password',COALESCE(config->>'cluster','') FROM newjitsu."ConfigurationObject" WHERE id=$1 AND "workspaceId"=$2 AND deleted=false`, destinationID, rs.Primary.Attributes["workspace_id"]).Scan(&protocol, &username, &database, &password, &cluster)
				if err != nil {
					return err
				}
				if protocol != "clickhouse-secure" || username != "default" || database != "default" || password != "" || cluster != "" {
					return fmt.Errorf("removed settings were not reset in Console storage")
				}
				return nil
			}},
			{Config: config(""), PlanOnly: true},
		},
	})
}

func TestAccDestination_resetRemovedPostgresPassword(t *testing.T) {
	suffix := testAccSuffix()
	destinationID := "destination_pg_reset_" + suffix
	config := func(destinationType, settings string) string {
		return fmt.Sprintf(`
%s
resource "jitsu_workspace" "test" {
 name = %q
 slug = %q
}
resource "jitsu_destination" "test" {
 workspace_id = jitsu_workspace.test.id
 id = %q
 name = "Reset postgres password"
 destination_type = %q
 %s
}
`, testAccProviderConfig(t), "Destination pg reset "+suffix, "destination-pg-reset-"+suffix, destinationID, destinationType, settings)
	}
	postgres := func(password string) string {
		return config("postgres", fmt.Sprintf(`postgres = {
 host = "postgres"
 database = "reporting"
 username = "reporting"
 %s
}`, password))
	}
	checkPassword := func(s *terraform.State) error {
		rs, err := testAccGetResourceState(s, "jitsu_destination.test")
		if err != nil {
			return err
		}
		db, err := sql.Open("postgres", testAccDatabaseURL())
		if err != nil {
			return err
		}
		defer db.Close()
		var password sql.NullString
		if err := db.QueryRow(`SELECT config->>'password' FROM newjitsu."ConfigurationObject" WHERE id=$1 AND "workspaceId"=$2 AND deleted=false`, destinationID, rs.Primary.Attributes["workspace_id"]).Scan(&password); err != nil {
			return err
		}
		if !password.Valid || password.String != "" {
			return fmt.Errorf("removed postgres password was not reset in Console storage")
		}
		return nil
	}
	clickhouse := config("clickhouse", `clickhouse = {
 hosts = ["clickhouse:8123"]
 password = "test-secret"
}`)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyRemote,
		Steps: []resource.TestStep{
			{Config: postgres(`password = "test-secret"`)},
			{Config: postgres(""), Check: checkPassword},
			{Config: postgres(""), PlanOnly: true},
			{Config: clickhouse},
			{Config: postgres(""), Check: checkPassword},
		},
	})
}

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
