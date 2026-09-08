package provider_test

import (
	"context"
	"fmt"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"os"
	"strings"
	"testing"
)

func TestAccLink_preservesFunctionOptions(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	suffix := testAccSuffix()
	config := testAccLinkConfigWithFunction(t, suffix, "options_stream_"+suffix, "options_dest_"+suffix, "options_function_"+suffix, true)
	changed := strings.Replace(config, "batch_size          = 10000", "batch_size          = 20000", 1)
	ctx := context.Background()
	c := testAccRemoteClient()
	defer c.Close()
	var linkID string
	find := func(s *terraform.State) (map[string]interface{}, error) {
		rs, err := testAccGetResourceState(s, "jitsu_link.test")
		if err != nil {
			return nil, err
		}
		links, err := c.List(ctx, rs.Primary.Attributes["workspace_id"], "link")
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			if link["id"] == rs.Primary.ID {
				return link, nil
			}
		}
		return nil, fmt.Errorf("link not found")
	}
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: testAccProtoV6ProviderFactories, CheckDestroy: testAccCheckDestroyRemote, Steps: []resource.TestStep{
		{Config: config, Check: func(s *terraform.State) error {
			link, err := find(s)
			if err != nil {
				return err
			}
			linkID = link["id"].(string)
			functions := link["data"].(map[string]interface{})["functions"].([]interface{})
			functions[0].(map[string]interface{})["functionOptions"] = map[string]interface{}{"target": "events_archive"}
			_, err = c.Create(ctx, link["workspaceId"].(string), "link", link)
			return err
		}},
		{Config: changed, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("jitsu_link.test", plancheck.ResourceActionUpdate)}}, Check: func(s *terraform.State) error {
			link, err := find(s)
			if err != nil {
				return err
			}
			if link["id"] != linkID {
				return fmt.Errorf("link ID changed")
			}
			functions := link["data"].(map[string]interface{})["functions"].([]interface{})
			options, ok := functions[0].(map[string]interface{})["functionOptions"].(map[string]interface{})
			if !ok || options["target"] != "events_archive" {
				return fmt.Errorf("batch size update erased function options")
			}
			return nil
		}},
		{Config: changed, PlanOnly: true},
		{Config: strings.Replace(changed, "[jitsu_function.link_test.id]", "[]", 1), Check: testAccCheckLinkRemoteFunctions("jitsu_link.test")},
		{Config: strings.Replace(changed, "[jitsu_function.link_test.id]", "[]", 1), PlanOnly: true},
	}})
}
