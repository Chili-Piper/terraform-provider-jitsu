package resources

import (
	"context"
	"reflect"
	"testing"

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
