package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func createRollbackTestState(t *testing.T, r resource.Resource, model interface{}) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	state := tfsdk.State{Schema: schema.Schema}
	if d := state.Set(ctx, model); d.HasError() {
		t.Fatal(d)
	}
	return state
}

func createRollbackTestKeys(t *testing.T, ids ...string) types.List {
	t.Helper()
	keys := make([]streamKeyModel, len(ids))
	for i, id := range ids {
		keys[i] = streamKeyModel{ID: types.StringValue(id), Plaintext: types.StringValue("secret-" + id)}
	}
	list, d := types.ListValueFrom(context.Background(), types.ObjectType{AttrTypes: streamKeyAttrTypes}, keys)
	if d.HasError() {
		t.Fatal(d)
	}
	return list
}

func TestAdversarialCreateRollbackState(t *testing.T) {
	for _, kind := range []string{"stream", "workspace"} {
		for _, rollbackFails := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/rollbackFails=%t", kind, rollbackFails), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					switch req.Method {
					case http.MethodPost:
						json.NewEncoder(w).Encode(map[string]string{"id": "created"})
					case http.MethodPut:
						http.Error(w, "injected update outage", 503)
					case http.MethodDelete:
						if rollbackFails {
							http.Error(w, "injected delete outage", 503)
						} else {
							json.NewEncoder(w).Encode(map[string]bool{"success": true})
						}
					}
				}))
				defer server.Close()
				c := client.New(server.URL, "test", "", "test")
				var r resource.Resource
				var model interface{}
				if kind == "stream" {
					r = &streamResource{client: c}
					model = &streamModel{WorkspaceID: types.StringValue("ws"), ID: types.StringValue("created"), Name: types.StringValue("Stream"), PublicKeys: createRollbackTestKeys(t, "managed"), PrivateKeys: createRollbackTestKeys(t)}
				} else {
					r = &workspaceResource{client: c}
					model = &workspaceModel{ID: types.StringUnknown(), Name: types.StringValue("Workspace"), Slug: types.StringValue("workspace")}
				}
				planned := createRollbackTestState(t, r, model)
				resp := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
				r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan{Raw: planned.Raw, Schema: planned.Schema}}, &resp)
				if !resp.Diagnostics.HasError() {
					t.Fatal("expected injected failure")
				}
				if rollbackFails && resp.State.Raw.IsNull() {
					t.Fatal("created object survived rollback but provider discarded its state")
				}
				if !rollbackFails && !resp.State.Raw.IsNull() {
					t.Fatal("successful rollback left a tracked object")
				}
			})
		}
	}
}
