package provider_test

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccFunction_basic(t *testing.T) {
	suffix := testAccSuffix()
	functionID := "test_acc_func_" + suffix
	code := `export default async function(event) { return event; }`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDestroyRemote,
		Steps: []resource.TestStep{
			// Create and Read
			{
				Config: testAccFunctionConfig(t, suffix, functionID, "Test Function", code),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_function.test", "name", "Test Function"),
					resource.TestCheckResourceAttr("jitsu_function.test", "id", functionID),
					resource.TestCheckResourceAttrPair("jitsu_function.test", "workspace_id", "jitsu_workspace.test", "id"),
					testAccCheckFunctionRemote("jitsu_function.test", "Test Function", code),
				),
			},
			// Update name
			{
				Config: testAccFunctionConfig(t, suffix, functionID, "Updated Function", code),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("jitsu_function.test", "name", "Updated Function"),
					testAccCheckFunctionRemote("jitsu_function.test", "Updated Function", code),
				),
			},
			// Import
			{
				ResourceName: "jitsu_function.test",
				ImportState:  true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					wsID := s.RootModule().Resources["jitsu_workspace.test"].Primary.ID
					return wsID + "/" + functionID, nil
				},
				ImportStateVerify: true,
			},
		},
	})
}

func testAccFunctionConfig(t *testing.T, suffix, functionID, name, code string) string {
	providerConfig := testAccProviderConfig(t)
	return fmt.Sprintf(`
%s

resource "jitsu_workspace" "test" {
  name = %q
  slug = %q
}

resource "jitsu_function" "test" {
  workspace_id = jitsu_workspace.test.id
  id           = %q
  name         = %q
  code         = %q
}
`, providerConfig, testAccWorkspaceName("TF Function Workspace", suffix), testAccWorkspaceSlug("tf-acc-func", suffix), functionID, name, code)
}
