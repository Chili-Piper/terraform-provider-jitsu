package provider_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccStream_rejectsEmptyKeyIDs(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	for _, field := range []string{"public_keys", "private_keys"} {
		t.Run(field, func(t *testing.T) {
			ctx := context.Background()
			c := testAccRemoteClient()
			defer c.Close()
			suffix := testAccSuffix()
			ws, err := c.WorkspaceCreate(ctx, "Empty key ID test", "empty-key-"+suffix)
			if err != nil {
				t.Fatal(err)
			}
			defer c.WorkspaceDelete(ctx, ws)
			id := "empty_key_" + suffix
			defer c.Delete(ctx, ws, "stream", id)
			otherField := "private_keys"
			if field == otherField {
				otherField = "public_keys"
			}
			config := func(keys string) string {
				return fmt.Sprintf(`
%s
resource "jitsu_stream" "test" {
 workspace_id = %q
 id = %q
 name = "Empty key ID test"
 %s = %s
 %s = [{id = "first", plaintext = "other-secret"}]
}
`, testAccProviderConfig(t), ws, id, field, keys, otherField)
			}
			valid := config(`[{id = "first", plaintext = "first-secret"}, {id = "second", plaintext = "second-secret"}]`)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{Config: config(`[{id = "", plaintext = "first-secret"}]`), ExpectError: regexp.MustCompile("key IDs must not be empty")},
					{Config: valid, PreConfig: func() {
						remote, err := c.Read(ctx, ws, "stream", id)
						if err != nil {
							t.Fatal(err)
						}
						if remote != nil {
							t.Fatal("invalid key IDs created a remote stream")
						}
					}},
					{Config: valid, PlanOnly: true},
				},
			})
		})
	}
}

func TestAccStream_repairsEmptyKeyID(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	for field, remoteField := range map[string]string{"public_keys": "publicKeys", "private_keys": "privateKeys"} {
		t.Run(field, func(t *testing.T) {
			ctx := context.Background()
			c := testAccRemoteClient()
			defer c.Close()
			suffix := testAccSuffix()
			ws, err := c.WorkspaceCreate(ctx, "Repair empty key ID", "repair-empty-key-"+suffix)
			if err != nil {
				t.Fatal(err)
			}
			defer c.WorkspaceDelete(ctx, ws)
			id := "repair_empty_key_" + suffix
			defer c.Delete(ctx, ws, "stream", id)
			config := fmt.Sprintf(`
%s
resource "jitsu_stream" "test" {
 workspace_id = %q
 id = %q
 name = "Repair empty key ID"
 %s = [{id = "managed", plaintext = "managed-secret"}]
}
`, testAccProviderConfig(t), ws, id, field)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{Config: config},
					{Config: config, PreConfig: func() {
						_, err := c.Update(ctx, ws, "stream", id, map[string]interface{}{
							remoteField: []map[string]string{{"id": "", "plaintext": "broken-secret"}},
						})
						if err != nil {
							t.Fatal(err)
						}
					}, ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("jitsu_stream.test", plancheck.ResourceActionUpdate),
					}}, Check: func(_ *terraform.State) error {
						remote, err := c.Read(ctx, ws, "stream", id)
						if err != nil {
							return err
						}
						keys, ok := remote[remoteField].([]interface{})
						if !ok || len(keys) != 1 {
							return fmt.Errorf("expected one repaired key, got %v", remote[remoteField])
						}
						key := keys[0].(map[string]interface{})
						if key["id"] != "managed" || key["hint"] != "man*ret" {
							return fmt.Errorf("empty-ID key was not replaced: %v", key)
						}
						return nil
					}},
					{Config: config, PlanOnly: true},
				},
			})
		})
	}
}
