package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestKeysToPayloadRejectsDuplicateIDs(t *testing.T) {
	ctx := context.Background()
	keys, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: streamKeyAttrTypes}, []streamKeyModel{
		{ID: types.StringValue("duplicate"), Plaintext: types.StringValue("first-secret")},
		{ID: types.StringValue("duplicate"), Plaintext: types.StringValue("second-secret")},
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	payload, err := keysToPayload(ctx, keys)
	if err == nil {
		remote := []interface{}{map[string]interface{}{"id": "duplicate"}, map[string]interface{}{"id": "duplicate"}}
		refreshed, diags := refreshStreamKeys(ctx, keys, remote)
		if diags.HasError() {
			t.Fatal(diags)
		}
		t.Fatalf("duplicate IDs reached the API (%d keys), then refresh changed plaintext state: before=%s after=%s", len(payload), keys, refreshed)
	}
	if !strings.Contains(err.Error(), "key IDs must be unique") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestStreamKeyIDUniquenessValidation(t *testing.T) {
	keyType := types.ObjectType{AttrTypes: streamKeyAttrTypes}
	keys := func(ids ...types.String) types.List {
		values := make([]attr.Value, len(ids))
		for i, id := range ids {
			values[i] = types.ObjectValueMust(streamKeyAttrTypes, map[string]attr.Value{
				"id": id, "plaintext": types.StringUnknown(),
			})
		}
		return types.ListValueMust(keyType, values)
	}
	for _, tc := range []struct {
		name      string
		keys      types.List
		wantError bool
	}{
		{"omitted", types.ListNull(keyType), false},
		{"unknown list", types.ListUnknown(keyType), false},
		{"unique IDs", keys(types.StringValue("first"), types.StringValue("second")), false},
		{"unknown IDs", keys(types.StringUnknown(), types.StringUnknown()), false},
		{"unknown object", types.ListValueMust(keyType, []attr.Value{types.ObjectUnknown(streamKeyAttrTypes)}), false},
		{"duplicate IDs", keys(types.StringValue("duplicate"), types.StringValue("duplicate")), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resp validator.ListResponse
			uniqueStreamKeyIDs{}.ValidateList(context.Background(), validator.ListRequest{ConfigValue: tc.keys}, &resp)
			if resp.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
		})
	}
}
