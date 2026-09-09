package provider_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccStream_preservesOriginRestrictions(t *testing.T) {
	suffix := testAccSuffix()
	config := testAccStreamConfig(t, suffix, "origin_"+suffix, "key_"+suffix, "Original")
	c := testAccRemoteClient()
	defer c.Close()
	var ws, id string
	resource.Test(t, resource.TestCase{ProtoV6ProviderFactories: testAccProtoV6ProviderFactories, CheckDestroy: testAccCheckDestroyRemote, Steps: []resource.TestStep{
		{Config: config, Check: func(s *terraform.State) error {
			rs := s.RootModule().Resources["jitsu_stream.test"].Primary
			ws, id = rs.Attributes["workspace_id"], rs.ID
			_, err := c.Update(context.Background(), ws, "stream", id, map[string]interface{}{"authorizedJavaScriptDomains": "https://trusted.example.com"})
			if err != nil {
				return err
			}
			remote, err := c.Read(context.Background(), ws, "stream", id)
			if err != nil {
				return err
			}
			if remote["authorizedJavaScriptDomains"] != "https://trusted.example.com" {
				return fmt.Errorf("fixture did not persist origin restriction: %v", remote["authorizedJavaScriptDomains"])
			}
			return nil
		}},
		{Config: strings.Replace(config, `name         = "Original"`, `name         = "Renamed"`, 1), Check: func(_ *terraform.State) error {
			remote, err := c.Read(context.Background(), ws, "stream", id)
			if err != nil {
				return err
			}
			if remote["authorizedJavaScriptDomains"] != "https://trusted.example.com" {
				return fmt.Errorf("rename removed browser-origin restriction: got %v", remote["authorizedJavaScriptDomains"])
			}
			return nil
		}},
		{Config: strings.Replace(config, `name         = "Original"`, `name         = "Renamed"`, 1), PlanOnly: true},
	}})
}
