package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func linkSettingsTestState(t *testing.T, r resource.Resource, model interface{}) tfsdk.State {
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

func TestAdversarialLinkPreservesUnmodeledSettings(t *testing.T) {
	var mu sync.Mutex
	remote := map[string]interface{}{"id": "link", "fromId": "stream", "toId": "destination", "data": map[string]interface{}{"batchSize": float64(100), "functionsEnv": map[string]interface{}{"REGION": "eu"}, "customFlag": true}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch req.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]interface{}{"links": []interface{}{remote}})
		case http.MethodPost:
			var payload map[string]interface{}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			remote["data"] = payload["data"]
			json.NewEncoder(w).Encode(map[string]string{"id": "link"})
		default:
			w.WriteHeader(405)
		}
	}))
	defer server.Close()
	r := &linkResource{client: client.New(server.URL, "test", "", "test")}
	model := linkModel{WorkspaceID: types.StringValue("ws"), ID: types.StringValue("link"), FromID: types.StringValue("stream"), ToID: types.StringValue("destination"), BatchSize: types.Int64Value(100), Functions: types.ListNull(types.StringType)}
	state := linkSettingsTestState(t, r, &model)
	model.BatchSize = types.Int64Null()
	planned := linkSettingsTestState(t, r, &model)
	resp := resource.UpdateResponse{State: state}
	r.Update(context.Background(), resource.UpdateRequest{State: state, Plan: tfsdk.Plan{Raw: planned.Raw, Schema: planned.Schema}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	mu.Lock()
	defer mu.Unlock()
	data := remote["data"].(map[string]interface{})
	if data["functionsEnv"] == nil || data["customFlag"] != true {
		t.Errorf("update erased remote-only settings: %v", data)
	}
	if _, ok := data["batchSize"]; ok {
		t.Error("removed managed setting was retained")
	}
}

func TestAccLinkPreservesSettingsConsole(t *testing.T) {
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
	t.Run("link environment", func(t *testing.T) {
		destID := "audit_dest_" + suffix
		if _, err := c.Create(ctx, ws, "destination", map[string]interface{}{"id": destID, "name": "Audit", "destinationType": "clickhouse", "protocol": "http", "hosts": []string{"clickhouse:8123"}, "username": "reporting", "password": "", "database": "default"}); err != nil {
			t.Fatal(err)
		}
		defer c.Delete(ctx, ws, "destination", destID)
		result, err := c.Create(ctx, ws, "link", map[string]interface{}{"fromId": streamID, "toId": destID, "type": "push", "data": map[string]interface{}{"batchSize": 100, "functionsEnv": map[string]string{"REGION": "eu"}}})
		if err != nil {
			t.Fatal(err)
		}
		id := result["id"].(string)
		defer c.DeleteLink(ctx, ws, id)
		r := &linkResource{client: c}
		model := linkModel{WorkspaceID: types.StringValue(ws), ID: types.StringValue(id), FromID: types.StringValue(streamID), ToID: types.StringValue(destID), BatchSize: types.Int64Value(100), Functions: types.ListNull(types.StringType)}
		state := linkSettingsTestState(t, r, &model)
		model.BatchSize = types.Int64Value(200)
		planned := linkSettingsTestState(t, r, &model)
		resp := resource.UpdateResponse{State: state}
		r.Update(ctx, resource.UpdateRequest{State: state, Plan: tfsdk.Plan{Raw: planned.Raw, Schema: planned.Schema}}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatal(resp.Diagnostics)
		}
		remote, err := r.findLinkByID(ctx, ws, id)
		if err != nil {
			t.Fatal(err)
		}
		data := remote["data"].(map[string]interface{})
		env, ok := data["functionsEnv"].(map[string]interface{})
		if !ok || env["REGION"] != "eu" {
			t.Fatalf("Console lost function environment after batch size update: %v", data)
		}
		if data["batchSize"] != float64(200) {
			t.Fatalf("batch size not updated: %v", data)
		}
	})
}
