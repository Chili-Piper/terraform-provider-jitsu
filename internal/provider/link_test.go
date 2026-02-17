package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccLink_basic(t *testing.T) {
	suffix := testAccSuffix()
	streamID := "site_test_acc_link_" + suffix
	destinationID := "dest_test_acc_link_" + suffix

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyRemote,
		Steps: []resource.TestStep{
			// Create (with prereq stream + destination) and Read
			{
				Config: testAccLinkConfig(t, suffix, streamID, destinationID, 5000),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_link.test", "mode", "batch"),
					resource.TestCheckResourceAttr("jitsu_link.test", "batch_size", "5000"),
					resource.TestCheckResourceAttr("jitsu_link.test", "data_layout", "segment-single-table"),
					resource.TestCheckResourceAttrSet("jitsu_link.test", "id"),
					testAccCheckLinkRemote("jitsu_link.test", "batch", "segment-single-table", 1, 5000),
				),
			},
			// Update batch_size
			{
				Config: testAccLinkConfig(t, suffix, streamID, destinationID, 10000),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_link.test", "batch_size", "10000"),
					testAccCheckLinkRemote("jitsu_link.test", "batch", "segment-single-table", 1, 10000),
				),
			},
			// Import by workspace_id/from_id/to_id
			{
				ResourceName: "jitsu_link.test",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					wsID := s.RootModule().Resources["jitsu_workspace.test"].Primary.ID
					return wsID + "/" + streamID + "/" + destinationID, nil
				},
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"functions"},
			},
		},
	})
}

func testAccLinkConfig(t *testing.T, suffix, streamID, destinationID string, batchSize int) string {
	providerConfig := testAccProviderConfig(t)
	return fmt.Sprintf(`
%s

resource "jitsu_workspace" "test" {
  name = %[2]q
  slug = %[3]q
}

resource "jitsu_stream" "link_test" {
  workspace_id = jitsu_workspace.test.id
  id           = %[4]q
  name         = "Link Test Stream"
}

resource "jitsu_destination" "link_test" {
  workspace_id     = jitsu_workspace.test.id
  id               = %[5]q
  name             = "Link Test Destination"
  destination_type = "clickhouse"
  protocol         = "http"
  hosts            = ["clickhouse:8123"]
  username         = "reporting"
  password         = ""
  database         = "default"
}

resource "jitsu_link" "test" {
  workspace_id = jitsu_workspace.test.id
  from_id      = jitsu_stream.link_test.id
  to_id        = jitsu_destination.link_test.id

  mode                = "batch"
  data_layout         = "segment-single-table"
  frequency           = 1
  batch_size          = %[6]d
  deduplicate         = true
  deduplicate_window  = 31
  schema_freeze       = false
  timestamp_column    = "timestamp"
  keep_original_names = false
}
`, providerConfig, testAccWorkspaceName("TF Link Workspace", suffix), testAccWorkspaceSlug("tf-acc-link", suffix), streamID, destinationID, batchSize)
}
