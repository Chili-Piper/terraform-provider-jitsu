package provider_test

import (
	"fmt"
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
				ImportStateVerifyIgnore: []string{"password"},
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
  protocol         = "http"
  hosts            = ["clickhouse:8123"]
  username         = "reporting"
  password         = ""
  database         = "default"
}
`, providerConfig, testAccWorkspaceName("TF Destination Workspace", suffix), testAccWorkspaceSlug("tf-acc-dest", suffix), destinationID, name)
}
