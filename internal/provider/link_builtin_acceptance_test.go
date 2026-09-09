package provider_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccLink_builtinFunctionID(t *testing.T) {
	suffix := testAccSuffix()
	config := testAccLinkConfigWithFunction(t, suffix, "builtin_stream_"+suffix, "builtin_dest_"+suffix, "function_"+suffix, true)
	config = strings.Replace(config, "[jitsu_function.link_test.id]", `["builtin.transformation.user-recognition"]`, 1)
	check := func(s *terraform.State) error {
		rs := s.RootModule().Resources["jitsu_link.test"].Primary
		c := testAccRemoteClient()
		defer c.Close()
		links, err := c.List(context.Background(), rs.Attributes["workspace_id"], "link")
		if err != nil {
			return err
		}
		for _, link := range links {
			if link["id"] == rs.ID {
				functions := link["data"].(map[string]interface{})["functions"].([]interface{})
				got := functions[0].(map[string]interface{})["functionId"]
				if got != "builtin.transformation.user-recognition" {
					return fmt.Errorf("built-in function replaced with nonexistent UDF: %v", got)
				}
				return nil
			}
		}
		return fmt.Errorf("link not found")
	}
	changed := strings.Replace(config, "batch_size          = 10000", "batch_size          = 20000", 1)
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: testAccProtoV6ProviderFactories, CheckDestroy: testAccCheckDestroyRemote, Steps: []resource.TestStep{
		{Config: config, Check: check}, {Config: changed, Check: check}, {Config: changed, PlanOnly: true},
	}})
}
