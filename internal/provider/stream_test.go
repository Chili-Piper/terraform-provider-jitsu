package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccStream_basic(t *testing.T) {
	suffix := testAccSuffix()
	streamID := "site_test_acc_" + suffix
	keyID := "js.test-acc-browser-" + suffix

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyRemote,
		Steps: []resource.TestStep{
			// Create with keys and Read
			{
				Config: testAccStreamConfig(t, suffix, streamID, keyID, "Test Stream"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_stream.test", "name", "Test Stream"),
					resource.TestCheckResourceAttr("jitsu_stream.test", "id", streamID),
					testAccCheckStreamRemote("jitsu_stream.test", "Test Stream", keyID),
				),
			},
			// Update name
			{
				Config: testAccStreamConfig(t, suffix, streamID, keyID, "Updated Stream"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_stream.test", "name", "Updated Stream"),
					testAccCheckStreamRemote("jitsu_stream.test", "Updated Stream", keyID),
				),
			},
			// Import (keys ignored — API returns hashed values)
			{
				ResourceName: "jitsu_stream.test",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					wsID := s.RootModule().Resources["jitsu_workspace.test"].Primary.ID
					return wsID + "/" + streamID, nil
				},
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"public_keys", "private_keys"},
			},
		},
	})
}

func testAccStreamConfig(t *testing.T, suffix, streamID, keyID, name string) string {
	providerConfig := testAccProviderConfig(t)
	return fmt.Sprintf(`
%s

resource "jitsu_workspace" "test" {
  name = %q
  slug = %q
}

resource "jitsu_stream" "test" {
  workspace_id = jitsu_workspace.test.id
  id           = %q
  name         = %q

  public_keys = [{
    id        = %q
    plaintext = %q
  }]
}
`, providerConfig, testAccWorkspaceName("TF Stream Workspace", suffix), testAccWorkspaceSlug("tf-acc-stream", suffix), streamID, name, keyID, keyID)
}
