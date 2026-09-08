package provider_test

import (
	"database/sql"
	"fmt"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"strings"
	"testing"
)

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
