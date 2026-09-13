package provider_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccStream_rejectsDuplicateKeyIDs(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	for _, field := range []string{"public_keys", "private_keys"} {
		t.Run(field, func(t *testing.T) {
			ctx := context.Background()
			c := testAccRemoteClient()
			defer c.Close()
			suffix := testAccSuffix()
			ws, err := c.WorkspaceCreate(ctx, "Duplicate key ID test", "duplicate-key-"+suffix)
			if err != nil {
				t.Fatal(err)
			}
			defer c.WorkspaceDelete(ctx, ws)
			id := "duplicate_key_" + suffix
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
 name = "Duplicate key ID test"
 %s = %s
 %s = [{id = "first", plaintext = "other-secret"}]
}
`, testAccProviderConfig(t), ws, id, field, keys, otherField)
			}
			valid := config(`[{id = "first", plaintext = "first-secret"}, {id = "second", plaintext = "second-secret"}]`)
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{Config: config(`[{id = "duplicate", plaintext = "first-secret"}, {id = "duplicate", plaintext = "second-secret"}]`), ExpectError: regexp.MustCompile("key IDs must be unique")},
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
