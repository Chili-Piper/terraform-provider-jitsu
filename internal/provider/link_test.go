package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccLink_basic(t *testing.T) {
	suffix := testAccSuffix()
	streamID := "site_test_acc_link_" + suffix
	destinationID := "dest_test_acc_link_" + suffix
	functionID := "fn_test_acc_link_" + suffix

	// The link id must survive updates: Kafka topic names and consumer groups
	// derive from it, so a replace would orphan the pipeline.
	var linkID string
	captureLinkID := resource.TestCheckResourceAttrWith("jitsu_link.test", "id", func(v string) error {
		linkID = v
		return nil
	})
	sameLinkID := resource.TestCheckResourceAttrWith("jitsu_link.test", "id", func(v string) error {
		if v != linkID {
			return fmt.Errorf("link was replaced: id changed from %q to %q", linkID, v)
		}
		return nil
	})
	expectInPlaceUpdate := resource.ConfigPlanChecks{
		PreApply: []plancheck.PlanCheck{
			plancheck.ExpectResourceAction("jitsu_link.test", plancheck.ResourceActionUpdate),
		},
	}

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
					captureLinkID,
					testAccCheckLinkRemote("jitsu_link.test", "batch", "segment-single-table", 1, 5000),
				),
			},
			// Update batch_size in place
			{
				Config:           testAccLinkConfig(t, suffix, streamID, destinationID, 10000),
				ConfigPlanChecks: expectInPlaceUpdate,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_link.test", "batch_size", "10000"),
					sameLinkID,
					testAccCheckLinkRemote("jitsu_link.test", "batch", "segment-single-table", 1, 10000),
				),
			},
			// Attach a function in place
			{
				Config:           testAccLinkConfigWithFunction(t, suffix, streamID, destinationID, functionID, true),
				ConfigPlanChecks: expectInPlaceUpdate,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_link.test", "functions.#", "1"),
					resource.TestCheckResourceAttr("jitsu_link.test", "functions.0", functionID),
					sameLinkID,
					testAccCheckLinkRemoteFunctions("jitsu_link.test", functionID),
				),
			},
			// Detach all functions in place; empty list must round-trip
			{
				Config:           testAccLinkConfigWithFunction(t, suffix, streamID, destinationID, functionID, false),
				ConfigPlanChecks: expectInPlaceUpdate,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_link.test", "functions.#", "0"),
					sameLinkID,
					testAccCheckLinkRemoteFunctions("jitsu_link.test"),
				),
			},
			// Empty functions list must not plan further changes
			{
				Config:   testAccLinkConfigWithFunction(t, suffix, streamID, destinationID, functionID, false),
				PlanOnly: true,
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
  clickhouse = {
    protocol = "http"
    hosts    = ["clickhouse:8123"]
    username = "reporting"
    password = ""
    database = "default"
  }
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

func testAccLinkConfigWithFunction(t *testing.T, suffix, streamID, destinationID, functionID string, attach bool) string {
	providerConfig := testAccProviderConfig(t)
	functionsExpr := "[]"
	if attach {
		functionsExpr = "[jitsu_function.link_test.id]"
	}
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
  clickhouse = {
    protocol = "http"
    hosts    = ["clickhouse:8123"]
    username = "reporting"
    password = ""
    database = "default"
  }
}

resource "jitsu_function" "link_test" {
  workspace_id = jitsu_workspace.test.id
  id           = %[6]q
  name         = "Link Test Function"
  code         = "export default async function (event, ctx) { return event; }"
}

resource "jitsu_link" "test" {
  workspace_id = jitsu_workspace.test.id
  from_id      = jitsu_stream.link_test.id
  to_id        = jitsu_destination.link_test.id

  mode                = "batch"
  data_layout         = "segment-single-table"
  frequency           = 1
  batch_size          = 10000
  deduplicate         = true
  deduplicate_window  = 31
  schema_freeze       = false
  timestamp_column    = "timestamp"
  keep_original_names = false
  functions           = %[7]s
}
`, providerConfig, testAccWorkspaceName("TF Link Workspace", suffix), testAccWorkspaceSlug("tf-acc-link", suffix), streamID, destinationID, functionID, functionsExpr)
}
