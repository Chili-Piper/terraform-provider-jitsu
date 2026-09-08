package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func streamDriftTestState(t *testing.T, r resource.Resource, model interface{}) tfsdk.State {
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

func streamDriftTestKeys(t *testing.T, ids ...string) types.List {
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

func TestAdversarialStreamKeyDrift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remote []map[string]string
		want   int
	}{
		{"revoked", []map[string]string{}, 0},
		{"unauthorized addition", []map[string]string{{"id": "managed"}, {"id": "external"}}, 2},
		{"unchanged", []map[string]string{{"id": "managed"}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				json.NewEncoder(w).Encode(map[string]interface{}{"id": "stream", "name": "Stream", "publicKeys": tc.remote, "privateKeys": tc.remote})
			}))
			defer server.Close()
			r := &streamResource{client: client.New(server.URL, "test", "", "test")}
			model := streamModel{WorkspaceID: types.StringValue("ws"), ID: types.StringValue("stream"), Name: types.StringValue("Stream"), PublicKeys: streamDriftTestKeys(t, "managed"), PrivateKeys: streamDriftTestKeys(t, "managed")}
			state := streamDriftTestState(t, r, &model)
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			var got streamModel
			if d := resp.State.Get(context.Background(), &got); d.HasError() {
				t.Fatal(d)
			}
			for _, list := range []types.List{got.PublicKeys, got.PrivateKeys} {
				if len(list.Elements()) != tc.want {
					t.Errorf("refresh reports %d keys; remote has %d", len(list.Elements()), tc.want)
				}
				var keys []streamKeyModel
				if d := list.ElementsAs(context.Background(), &keys, false); d.HasError() {
					t.Fatal(d)
				}
				for _, key := range keys {
					if key.ID.ValueString() == "managed" && key.Plaintext.ValueString() != "secret-managed" {
						t.Error("lost managed plaintext")
					}
					if key.ID.ValueString() == "external" && !key.Plaintext.IsNull() {
						t.Error("invented external plaintext")
					}
				}
			}
		})
	}
}

func TestAccStreamKeyDriftConsole(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	ctx := context.Background()
	c := client.New(os.Getenv("JITSU_CONSOLE_URL"), os.Getenv("JITSU_AUTH_TOKEN"), os.Getenv("JITSU_DATABASE_URL"), "adversarial-test")
	defer c.Close()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ws, err := c.WorkspaceCreate(ctx, "Adversarial "+suffix, "adversarial-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	defer c.WorkspaceDelete(ctx, ws)
	streamID := "audit_stream_" + suffix
	if _, err := c.Create(ctx, ws, "stream", map[string]interface{}{"id": streamID, "name": "Audit"}); err != nil {
		t.Fatal(err)
	}
	defer c.Delete(ctx, ws, "stream", streamID)
	t.Run("key drift", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			keys []map[string]string
		}{
			{"revoked", []map[string]string{}},
			{"added", []map[string]string{{"id": "managed", "plaintext": "secret-managed"}, {"id": "external", "plaintext": "secret-external"}}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := c.Update(ctx, ws, "stream", streamID, map[string]interface{}{"publicKeys": tc.keys, "privateKeys": tc.keys}); err != nil {
					t.Fatal(err)
				}
				r := &streamResource{client: c}
				model := streamModel{WorkspaceID: types.StringValue(ws), ID: types.StringValue(streamID), Name: types.StringValue("Audit"), PublicKeys: streamDriftTestKeys(t, "managed"), PrivateKeys: streamDriftTestKeys(t, "managed")}
				state := streamDriftTestState(t, r, &model)
				resp := resource.ReadResponse{State: state}
				r.Read(ctx, resource.ReadRequest{State: state}, &resp)
				if resp.Diagnostics.HasError() {
					t.Fatal(resp.Diagnostics)
				}
				if d := resp.State.Get(ctx, &model); d.HasError() {
					t.Fatal(d)
				}
				if len(model.PublicKeys.Elements()) != len(tc.keys) || len(model.PrivateKeys.Elements()) != len(tc.keys) {
					t.Fatalf("Console has %d keys per list; refreshed state has %d public, %d private", len(tc.keys), len(model.PublicKeys.Elements()), len(model.PrivateKeys.Elements()))
				}
			})
		}
	})
}
