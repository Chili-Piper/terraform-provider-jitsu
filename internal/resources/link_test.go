package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestReadLinkIntoState_ClearsMissingOptionalFields(t *testing.T) {
	ctx := context.Background()

	funcs, diags := types.ListValueFrom(ctx, types.StringType, []string{"existing_func"})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building functions list: %v", diags)
	}

	state := linkModel{
		Mode:              types.StringValue("batch"),
		DataLayout:        types.StringValue("segment-single-table"),
		PrimaryKey:        types.StringValue("id"),
		Frequency:         types.Int64Value(1),
		BatchSize:         types.Int64Value(1000),
		Deduplicate:       types.BoolValue(true),
		DeduplicateWindow: types.Int64Value(31),
		SchemaFreeze:      types.BoolValue(true),
		TimestampColumn:   types.StringValue("timestamp"),
		KeepOriginalNames: types.BoolValue(true),
		Functions:         funcs,
	}

	link := map[string]interface{}{
		"id":     "link_id",
		"fromId": "stream_id",
		"toId":   "destination_id",
		"data":   map[string]interface{}{},
	}

	diags = readLinkIntoState(ctx, link, &state)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if !state.Mode.IsNull() {
		t.Fatalf("mode should be null, got %v", state.Mode)
	}
	if !state.DataLayout.IsNull() {
		t.Fatalf("data_layout should be null, got %v", state.DataLayout)
	}
	if !state.PrimaryKey.IsNull() {
		t.Fatalf("primary_key should be null, got %v", state.PrimaryKey)
	}
	if !state.Frequency.IsNull() {
		t.Fatalf("frequency should be null, got %v", state.Frequency)
	}
	if !state.BatchSize.IsNull() {
		t.Fatalf("batch_size should be null, got %v", state.BatchSize)
	}
	if !state.Deduplicate.IsNull() {
		t.Fatalf("deduplicate should be null, got %v", state.Deduplicate)
	}
	if !state.DeduplicateWindow.IsNull() {
		t.Fatalf("deduplicate_window should be null, got %v", state.DeduplicateWindow)
	}
	if !state.SchemaFreeze.IsNull() {
		t.Fatalf("schema_freeze should be null, got %v", state.SchemaFreeze)
	}
	if !state.TimestampColumn.IsNull() {
		t.Fatalf("timestamp_column should be null, got %v", state.TimestampColumn)
	}
	if !state.KeepOriginalNames.IsNull() {
		t.Fatalf("keep_original_names should be null, got %v", state.KeepOriginalNames)
	}
	if !state.Functions.IsNull() {
		t.Fatalf("functions should be null, got %v", state.Functions)
	}
}

func TestReadLinkIntoState_ClearsSettingsWithoutData(t *testing.T) {
	ctx := context.Background()
	for name, link := range map[string]map[string]interface{}{
		"absent":     {"id": "link_id"},
		"null":       {"id": "link_id", "data": nil},
		"not object": {"id": "link_id", "data": []interface{}{"batch"}},
	} {
		t.Run(name, func(t *testing.T) {
			funcs, diags := types.ListValueFrom(ctx, types.StringType, []string{"existing_func"})
			if diags.HasError() {
				t.Fatalf("unexpected diagnostics building functions list: %v", diags)
			}
			state := linkModel{
				Mode:        types.StringValue("batch"),
				Frequency:   types.Int64Value(1),
				Deduplicate: types.BoolValue(true),
				Functions:   funcs,
			}
			if diags := readLinkIntoState(ctx, link, &state); diags.HasError() {
				t.Fatalf("unexpected diagnostics: %v", diags)
			}
			if !state.Mode.IsNull() || !state.Frequency.IsNull() || !state.Deduplicate.IsNull() || !state.Functions.IsNull() {
				t.Fatalf("settings should be null, got mode=%v frequency=%v deduplicate=%v functions=%v",
					state.Mode, state.Frequency, state.Deduplicate, state.Functions)
			}
		})
	}
}

func TestReadLinkIntoState_ParsesFields(t *testing.T) {
	ctx := context.Background()

	state := linkModel{}
	link := map[string]interface{}{
		"id":     "link_id",
		"fromId": "stream_id",
		"toId":   "destination_id",
		"data": map[string]interface{}{
			"mode":              "batch",
			"dataLayout":        "segment-single-table",
			"primaryKey":        "id",
			"frequency":         float64(2),
			"batchSize":         float64(5000),
			"deduplicate":       false,
			"deduplicateWindow": float64(31),
			"schemaFreeze":      true,
			"timestampColumn":   "timestamp",
			"keepOriginalNames": false,
			"functions": []interface{}{
				map[string]interface{}{"functionId": "udf.normalize"},
				map[string]interface{}{"functionId": "already_plain"},
			},
		},
	}

	diags := readLinkIntoState(ctx, link, &state)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if state.ID.ValueString() != "link_id" {
		t.Fatalf("id mismatch: got %q", state.ID.ValueString())
	}
	if state.FromID.ValueString() != "stream_id" {
		t.Fatalf("from_id mismatch: got %q", state.FromID.ValueString())
	}
	if state.ToID.ValueString() != "destination_id" {
		t.Fatalf("to_id mismatch: got %q", state.ToID.ValueString())
	}
	if state.Mode.ValueString() != "batch" {
		t.Fatalf("mode mismatch: got %q", state.Mode.ValueString())
	}
	if state.Frequency.ValueInt64() != 2 {
		t.Fatalf("frequency mismatch: got %d", state.Frequency.ValueInt64())
	}
	if state.BatchSize.ValueInt64() != 5000 {
		t.Fatalf("batch_size mismatch: got %d", state.BatchSize.ValueInt64())
	}
	if state.Deduplicate.ValueBool() {
		t.Fatalf("deduplicate mismatch: got true, want false")
	}
	if !state.SchemaFreeze.ValueBool() {
		t.Fatalf("schema_freeze mismatch: got false, want true")
	}

	var gotFunctions []string
	diags = state.Functions.ElementsAs(ctx, &gotFunctions, false)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics reading functions: %v", diags)
	}
	wantFunctions := []string{"normalize", "already_plain"}
	if !reflect.DeepEqual(gotFunctions, wantFunctions) {
		t.Fatalf("functions mismatch: got %v want %v", gotFunctions, wantFunctions)
	}
}

func TestPreserveFunctionSettings(t *testing.T) {
	remote := []interface{}{
		map[string]interface{}{"functionId": "udf.first", "functionOptions": map[string]interface{}{"position": "one"}},
		map[string]interface{}{"functionId": "udf.second", "functionOptions": map[string]interface{}{"position": "two"}},
		map[string]interface{}{"functionId": "udf.first", "functionOptions": map[string]interface{}{"position": "three"}},
		map[string]interface{}{"functionId": "udf.removed", "functionOptions": map[string]interface{}{"position": "removed"}},
	}
	got := preserveFunctionSettings([]map[string]string{{"functionId": "udf.second"}, {"functionId": "udf.first"}, {"functionId": "udf.first"}, {"functionId": "udf.new"}}, remote)
	want := []map[string]interface{}{
		remote[1].(map[string]interface{}), remote[0].(map[string]interface{}), remote[2].(map[string]interface{}), {"functionId": "udf.new"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reordering, duplicate occurrences, removal, and addition must preserve only matching options: %v", got)
	}
	if cleared := preserveFunctionSettings([]map[string]string{}, remote); cleared == nil || len(cleared) != 0 {
		t.Fatal("clearing functions must return an explicit empty list")
	}
	got[0]["functionId"] = "changed"
	if remote[1].(map[string]interface{})["functionId"] != "udf.second" {
		t.Fatal("changed remote source map")
	}
}

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

func TestAccLinkRejectsInvalidDataLayout(t *testing.T) {
	c, ws := auditConsole(t)
	ctx := context.Background()
	streamID, destinationID := "layout_stream_"+ws, "layout_destination_"+ws
	if _, err := c.Create(ctx, ws, "stream", map[string]interface{}{"id": streamID, "name": "Layout regression"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Delete(ctx, ws, "stream", streamID) })
	if _, err := c.Create(ctx, ws, "destination", map[string]interface{}{
		"id": destinationID, "name": "Layout regression", "destinationType": "clickhouse",
		"protocol": "http", "hosts": []string{"clickhouse:8123"}, "username": "default", "password": "", "database": "default",
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Delete(ctx, ws, "destination", destinationID) })
	r := &linkResource{client: c}
	model := linkModel{
		WorkspaceID: types.StringValue(ws), ID: types.StringUnknown(), FromID: types.StringValue(streamID),
		ToID: types.StringValue(destinationID), DataLayout: types.StringValue("segment_single_table"), Functions: types.ListNull(types.StringType),
	}
	checkLayoutDiagnostic := func(t *testing.T, diagnostics diag.Diagnostics) {
		t.Helper()
		for _, diagnostic := range diagnostics.Errors() {
			if strings.Contains(diagnostic.Detail(), "data_layout must be") {
				return
			}
		}
		t.Fatalf("expected data_layout rejection, got %v", diagnostics)
	}
	t.Run("create", func(t *testing.T) {
		planned := linkSettingsTestState(t, r, &model)
		resp := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
		r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Raw: planned.Raw, Schema: planned.Schema}}, &resp)
		remote, err := r.findLink(ctx, ws, streamID, destinationID)
		if err != nil {
			t.Fatal(err)
		}
		if remote != nil {
			defer c.DeleteLink(ctx, ws, remote["id"].(string))
		}
		if !resp.Diagnostics.HasError() {
			t.Fatalf("create reported success and Console persisted unusable dataLayout=%q", remote["data"].(map[string]interface{})["dataLayout"])
		}
		checkLayoutDiagnostic(t, resp.Diagnostics)
		if remote != nil {
			t.Fatal("rejected create still persisted a link")
		}
		t.Log("Invalid data layout rejected before Console creation; no link persisted")
	})
	t.Run("update", func(t *testing.T) {
		created, err := c.Create(ctx, ws, "link", map[string]interface{}{
			"fromId": streamID, "toId": destinationID, "type": "push", "data": map[string]interface{}{"dataLayout": "segment-single-table"},
		})
		if err != nil {
			t.Fatal(err)
		}
		id := created["id"].(string)
		defer c.DeleteLink(ctx, ws, id)
		model.ID = types.StringValue(id)
		model.DataLayout = types.StringValue("segment-single-table")
		state := linkSettingsTestState(t, r, &model)
		model.DataLayout = types.StringValue("segment_single_table")
		planned := linkSettingsTestState(t, r, &model)
		resp := resource.UpdateResponse{State: state}
		r.Update(ctx, resource.UpdateRequest{State: state, Plan: tfsdk.Plan{Raw: planned.Raw, Schema: planned.Schema}}, &resp)
		remote, err := r.findLinkByID(ctx, ws, id)
		if err != nil {
			t.Fatal(err)
		}
		layout := remote["data"].(map[string]interface{})["dataLayout"]
		if !resp.Diagnostics.HasError() {
			t.Fatalf("update reported success and Console persisted unusable dataLayout=%q", layout)
		}
		checkLayoutDiagnostic(t, resp.Diagnostics)
		if layout != "segment-single-table" {
			t.Fatalf("rejected update changed the working data layout to %q", layout)
		}
		t.Log("Invalid data layout rejected; Console still contains the working segment-single-table layout")
	})
}
