package provider_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccStream_repairsKeyDrift(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	ctx := context.Background()
	c := testAccRemoteClient()
	defer c.Close()
	suffix := testAccSuffix()
	ws, err := c.WorkspaceCreate(ctx, "Key drift test", "key-drift-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	defer c.WorkspaceDelete(ctx, ws)
	id := "key_drift_" + suffix
	config := fmt.Sprintf(`
%s
resource "jitsu_stream" "test" {
 workspace_id = %q
 id = %q
 name = "Key drift test"
 public_keys = [{id = "managed_public", plaintext = "public-secret"}]
 private_keys = [{id = "managed_private", plaintext = "private-secret"}]
}
`, testAccProviderConfig(t), ws, id)
	drift := func(add bool) func() {
		return func() {
			payload := map[string]interface{}{}
			for field, managed := range map[string]string{"publicKeys": "managed_public", "privateKeys": "managed_private"} {
				keys := []map[string]string{}
				if add {
					keys = append(keys, map[string]string{"id": managed, "plaintext": "managed-secret"}, map[string]string{"id": "external", "plaintext": "external-secret"})
				}
				payload[field] = keys
			}
			if _, err := c.Update(ctx, ws, "stream", id, payload); err != nil {
				t.Fatal(err)
			}
		}
	}
	check := func(_ *terraform.State) error {
		remote, err := c.Read(ctx, ws, "stream", id)
		if err != nil {
			return err
		}
		for field, managed := range map[string]string{"publicKeys": "managed_public", "privateKeys": "managed_private"} {
			keys, ok := remote[field].([]interface{})
			if !ok || len(keys) != 1 || keys[0].(map[string]interface{})["id"] != managed {
				return fmt.Errorf("%s drift was not repaired", field)
			}
		}
		return nil
	}
	update := resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("jitsu_stream.test", plancheck.ResourceActionUpdate)}}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config, Check: check},
			{Config: config, PreConfig: drift(false), ConfigPlanChecks: update, Check: check},
			{Config: config, PlanOnly: true},
			{Config: config, PreConfig: drift(true), ConfigPlanChecks: update, Check: check},
			{Config: config, PlanOnly: true},
		},
	})
}
