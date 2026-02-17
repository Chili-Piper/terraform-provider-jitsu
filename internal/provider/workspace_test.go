package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccWorkspace_basic(t *testing.T) {
	suffix := testAccSuffix()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyRemote,
		Steps: []resource.TestStep{
			{
				Config: testAccWorkspaceConfig(t, suffix, "Test Workspace", testAccWorkspaceSlug("tf-acc-ws", suffix)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_workspace.test", "name", testAccWorkspaceName("Test Workspace", suffix)),
					resource.TestCheckResourceAttr("jitsu_workspace.test", "slug", testAccWorkspaceSlug("tf-acc-ws", suffix)),
					resource.TestCheckResourceAttrSet("jitsu_workspace.test", "id"),
					testAccCheckWorkspaceRemote(
						"jitsu_workspace.test",
						testAccWorkspaceName("Test Workspace", suffix),
						testAccWorkspaceSlug("tf-acc-ws", suffix),
					),
				),
			},
			{
				Config: testAccWorkspaceConfig(t, suffix, "Updated Workspace", testAccWorkspaceSlug("tf-acc-ws-upd", suffix)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_workspace.test", "name", testAccWorkspaceName("Updated Workspace", suffix)),
					resource.TestCheckResourceAttr("jitsu_workspace.test", "slug", testAccWorkspaceSlug("tf-acc-ws-upd", suffix)),
					testAccCheckWorkspaceRemote(
						"jitsu_workspace.test",
						testAccWorkspaceName("Updated Workspace", suffix),
						testAccWorkspaceSlug("tf-acc-ws-upd", suffix),
					),
				),
			},
			{
				ResourceName:      "jitsu_workspace.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccWorkspaceConfig(t *testing.T, suffix, name, slug string) string {
	providerConfig := testAccProviderConfig(t)
	return fmt.Sprintf(`
%s

resource "jitsu_workspace" "test" {
  name = %q
  slug = %q
}
`, providerConfig, testAccWorkspaceName(name, suffix), slug)
}
