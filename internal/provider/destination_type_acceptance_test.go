package provider_test

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

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
