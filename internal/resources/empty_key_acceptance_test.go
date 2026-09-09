package resources

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestAccStream_emptyKeyDoesNotSilentlyRetainCredential(t *testing.T) {
	c, ws := auditConsole(t)
	ctx := context.Background()
	db, err := sql.Open("postgres", os.Getenv("JITSU_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, field := range []string{"publicKeys", "privateKeys"} {
		t.Run(field, func(t *testing.T) {
			id := "empty_" + field + "_" + ws
			_, err := c.Create(ctx, ws, "stream", map[string]interface{}{"id": id, "name": "Empty key test"})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Delete(ctx, ws, "stream", id)
			_, err = c.Update(ctx, ws, "stream", id, map[string]interface{}{field: []map[string]string{{"id": "managed", "plaintext": "previous-working-secret", "hint": "pre*ret"}}})
			if err != nil {
				t.Fatal(err)
			}
			readHash := func() string {
				var hash string
				if err := db.QueryRowContext(ctx, `SELECT config->$1->0->>'hash' FROM newjitsu."ConfigurationObject" WHERE id=$2 AND "workspaceId"=$3`, field, id, ws).Scan(&hash); err != nil {
					t.Fatal(err)
				}
				return hash
			}
			before := readHash()
			r := &streamResource{client: c}
			keyType := types.ObjectType{AttrTypes: streamKeyAttrTypes}
			model := streamModel{WorkspaceID: types.StringValue(ws), ID: types.StringValue(id), Name: types.StringValue("Empty key test"), PublicKeys: types.ListNull(keyType), PrivateKeys: types.ListNull(keyType)}
			keys := func(secret string) types.List {
				value, d := types.ListValueFrom(ctx, keyType, []streamKeyModel{{ID: types.StringValue("managed"), Plaintext: types.StringValue(secret)}})
				if d.HasError() {
					t.Fatal(d)
				}
				return value
			}
			if field == "publicKeys" {
				model.PublicKeys = keys("previous-working-secret")
			} else {
				model.PrivateKeys = keys("previous-working-secret")
			}
			state := createRollbackTestState(t, r, &model)
			if field == "publicKeys" {
				model.PublicKeys = keys("")
			} else {
				model.PrivateKeys = keys("")
			}
			plan := createRollbackTestState(t, r, &model)
			resp := resource.UpdateResponse{State: state}
			r.Update(ctx, resource.UpdateRequest{State: state, Plan: tfsdk.Plan{Raw: plan.Raw, Schema: plan.Schema}}, &resp)
			unchanged := readHash() == before
			if !resp.Diagnostics.HasError() {
				t.Fatalf("empty %s rotation reported success; previous credential hash unchanged=%t", field, unchanged)
			}
			if !unchanged {
				t.Fatal("rejected update changed the credential")
			}
			t.Log("Empty rotation rejected and previous credential left unchanged")
		})
	}
}
