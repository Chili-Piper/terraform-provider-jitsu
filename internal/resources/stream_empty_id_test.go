package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestKeysToPayloadRejectsEmptyID(t *testing.T) {
	ctx := context.Background()
	keys, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: streamKeyAttrTypes}, []streamKeyModel{
		{ID: types.StringValue(""), Plaintext: types.StringValue("first-secret")},
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	if _, err := keysToPayload(ctx, keys); err == nil || !strings.Contains(err.Error(), "key IDs must not be empty") {
		t.Fatalf("expected empty ID to be rejected before mutation, got %v", err)
	}
}

func TestStreamKeyIDValidationDefersUnknown(t *testing.T) {
	ctx := context.Background()
	var resp validator.StringResponse
	nonEmptyStreamKeyID{}.ValidateString(ctx, validator.StringRequest{ConfigValue: types.StringUnknown()}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	keys, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: streamKeyAttrTypes}, []streamKeyModel{
		{ID: types.StringUnknown(), Plaintext: types.StringValue("first-secret")},
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	if _, err := keysToPayload(ctx, keys); err == nil || !strings.Contains(err.Error(), "key IDs must be known before applying") {
		t.Fatalf("expected unresolved ID to be rejected at apply, got %v", err)
	}
}

func TestStreamReadExistingEmptyKeyID(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remote    map[string]interface{}
		wantError bool
	}{
		{"existing empty ID", map[string]interface{}{"id": ""}, false},
		{"missing ID", map[string]interface{}{}, true},
		{"non-string ID", map[string]interface{}{"id": 42}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				json.NewEncoder(w).Encode(map[string]interface{}{"id": "stream", "name": "Stream", "publicKeys": []interface{}{tc.remote}})
			}))
			defer server.Close()
			r := &streamResource{client: client.New(server.URL, "test", "", "test")}
			model := streamModel{
				WorkspaceID: types.StringValue("ws"), ID: types.StringValue("stream"), Name: types.StringValue("Stream"),
				PublicKeys: streamDriftTestKeys(t, ""), PrivateKeys: types.ListNull(types.ObjectType{AttrTypes: streamKeyAttrTypes}),
			}
			state := streamDriftTestState(t, r, &model)
			resp := resource.ReadResponse{State: state}
			r.Read(ctx, resource.ReadRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("unexpected refresh diagnostics for existing empty-ID state: %v", resp.Diagnostics)
			}
			if tc.wantError {
				return
			}
			var got streamModel
			if diags := resp.State.Get(ctx, &got); diags.HasError() {
				t.Fatal(diags)
			}
			if !got.PublicKeys.Equal(model.PublicKeys) {
				t.Fatalf("refresh changed existing key state: %s", got.PublicKeys)
			}
		})
	}
}
