package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

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
