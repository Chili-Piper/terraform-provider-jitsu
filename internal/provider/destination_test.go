package provider_test

import (
	"fmt"
	"regexp"
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
