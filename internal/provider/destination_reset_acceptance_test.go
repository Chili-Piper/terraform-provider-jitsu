package provider_test

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

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
