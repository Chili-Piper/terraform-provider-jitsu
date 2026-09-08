package provider_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccStream_importPreservesKeys(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	ctx := context.Background()
	c := testAccRemoteClient()
	defer c.Close()
	suffix := testAccSuffix()
	workspaceID, err := c.WorkspaceCreate(ctx, "Stream import test", "stream-import-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	defer c.WorkspaceDelete(ctx, workspaceID)
	streamID := "stream_import_" + suffix
	payload := map[string]interface{}{"id": streamID, "workspaceId": workspaceID, "type": "stream", "name": "Before"}
	if _, err := c.Create(ctx, workspaceID, "stream", payload); err != nil {
		t.Fatal(err)
	}
	defer c.Delete(ctx, workspaceID, "stream", streamID)
	payload["publicKeys"] = []map[string]string{{"id": "public_" + suffix, "plaintext": "public-secret-" + suffix}}
	payload["privateKeys"] = []map[string]string{{"id": "private_" + suffix, "plaintext": "private-secret-" + suffix}}
	if _, err := c.Update(ctx, workspaceID, "stream", streamID, payload); err != nil {
		t.Fatal(err)
	}
	config := func(name, keys string) string {
		return fmt.Sprintf(`
%s
resource "jitsu_stream" "test" {
 workspace_id = %q
 id = %q
 name = %q
 %s
}
`, testAccProviderConfig(t), workspaceID, streamID, name, keys)
	}
	check := func(empty bool) resource.TestCheckFunc {
		return func(_ *terraform.State) error {
			result, err := c.Read(ctx, workspaceID, "stream", streamID)
			if err != nil {
				return err
			}
			for field, id := range map[string]string{"publicKeys": "public_" + suffix, "privateKeys": "private_" + suffix} {
				keys, ok := result[field].([]interface{})
				if !ok {
					return fmt.Errorf("missing %s", field)
				}
				if empty {
					if len(keys) != 0 {
						return fmt.Errorf("%s were not revoked", field)
					}
					continue
				}
				if len(keys) != 1 || keys[0].(map[string]interface{})["id"] != id {
					return fmt.Errorf("%s changed during rename", field)
				}
			}
			return nil
		}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: config("Before", ""), ResourceName: "jitsu_stream.test", ImportState: true, ImportStateId: workspaceID + "/" + streamID, ImportStatePersist: true},
			{Config: config("After", ""), Check: check(false)},
			{Config: config("After", ""), PlanOnly: true},
			{Config: config("After", "public_keys = []\nprivate_keys = []"), Check: check(true)},
			{Config: config("After", "public_keys = []\nprivate_keys = []"), PlanOnly: true},
		},
	})
}
