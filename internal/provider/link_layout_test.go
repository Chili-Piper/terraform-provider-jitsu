package provider_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestLink_dataLayoutValidation(t *testing.T) {
	config := func(layout string) string {
		return fmt.Sprintf(`
%s
resource "jitsu_link" "test" {
  workspace_id = "layout-validation"
  from_id = "stream"
  to_id = "destination"
  data_layout = %s
}
`, testAccProviderConfig(t), layout)
	}
	steps := []resource.TestStep{}
	for _, layout := range []string{"segment", "segment-single-table", "jitsu-legacy", "passthrough"} {
		steps = append(steps, resource.TestStep{Config: config(fmt.Sprintf("%q", layout)), PlanOnly: true, ExpectNonEmptyPlan: true})
	}
	steps = append(steps, resource.TestStep{Config: config("null"), PlanOnly: true, ExpectNonEmptyPlan: true})
	for _, layout := range []string{"segment_single_table", "", "toString"} {
		steps = append(steps, resource.TestStep{Config: config(fmt.Sprintf("%q", layout)), PlanOnly: true, ExpectError: regexp.MustCompile("Invalid data layout")})
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    steps,
	})
}
