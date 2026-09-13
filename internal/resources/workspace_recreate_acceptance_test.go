package resources

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestAccWorkspaceRecreateDeletedSlug(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	c := client.New(os.Getenv("JITSU_CONSOLE_URL"), os.Getenv("JITSU_AUTH_TOKEN"), os.Getenv("JITSU_DATABASE_URL"), "workspace-recreate-test")
	t.Cleanup(c.Close)
	ctx := context.Background()
	r := &workspaceResource{client: c}
	slug := fmt.Sprintf("recreate-%d", time.Now().UnixNano())
	model := &workspaceModel{ID: types.StringUnknown(), Name: types.StringValue("Recreated workspace"), Slug: types.StringValue(slug)}
	plan := createRollbackTestState(t, r, model)
	create := func() resource.CreateResponse {
		resp := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
		r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Raw: plan.Raw, Schema: plan.Schema}}, &resp)
		return resp
	}
	first := create()
	if first.Diagnostics.HasError() {
		t.Fatalf("initial create failed: %v", first.Diagnostics)
	}
	var firstModel workspaceModel
	if d := first.State.Get(ctx, &firstModel); d.HasError() {
		t.Fatal(d)
	}
	t.Cleanup(func() { c.WorkspaceDelete(context.Background(), firstModel.ID.ValueString()) })
	functionID := "retained_" + firstModel.ID.ValueString()
	if _, err := c.Create(ctx, firstModel.ID.ValueString(), "function", map[string]interface{}{
		"id": functionID, "name": "Retained function", "code": "export default function(event) { return event; }",
	}); err != nil {
		t.Fatal(err)
	}
	deleted := resource.DeleteResponse{}
	r.Delete(ctx, resource.DeleteRequest{State: first.State}, &deleted)
	if deleted.Diagnostics.HasError() {
		t.Fatal(deleted.Diagnostics)
	}
	second := create()
	if second.Diagnostics.HasError() {
		t.Fatalf("workspace destroy succeeded, but recreating the same slug failed: %v", second.Diagnostics)
	}
	var secondModel workspaceModel
	if d := second.State.Get(ctx, &secondModel); d.HasError() {
		t.Fatal(d)
	}
	t.Cleanup(func() { c.WorkspaceDelete(context.Background(), secondModel.ID.ValueString()) })
	if secondModel.ID.Equal(firstModel.ID) {
		t.Fatal("recreating a workspace must not revive the deleted workspace and its data")
	}
	remote, err := c.WorkspaceRead(ctx, secondModel.ID.ValueString())
	if err != nil {
		t.Fatal(err)
	}
	if remote == nil || remote["slug"] != slug {
		t.Fatalf("recreated workspace does not have requested slug: %#v", remote)
	}
	db, err := sql.Open("postgres", os.Getenv("JITSU_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var oldDeleted bool
	var oldSlug sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT deleted,slug FROM newjitsu."Workspace" WHERE id=$1`, firstModel.ID.ValueString()).Scan(&oldDeleted, &oldSlug); err != nil {
		t.Fatal(err)
	}
	if !oldDeleted || oldSlug.Valid {
		t.Fatalf("old workspace must remain deleted with a released slug: deleted=%t slug=%v", oldDeleted, oldSlug)
	}
	var functionWorkspace string
	if err := db.QueryRowContext(ctx, `SELECT "workspaceId" FROM newjitsu."ConfigurationObject" WHERE id=$1`, functionID).Scan(&functionWorkspace); err != nil {
		t.Fatal(err)
	}
	if functionWorkspace != firstModel.ID.ValueString() {
		t.Fatal("recovery moved the deleted workspace's configuration")
	}
	oldFunction, err := c.Read(ctx, secondModel.ID.ValueString(), "function", functionID)
	if err != nil {
		t.Fatal(err)
	}
	if oldFunction != nil {
		t.Fatal("recreated workspace inherited the deleted workspace's configuration")
	}
	t.Logf("deleted workspace %s; recreated slug %s on new workspace %s", firstModel.ID.ValueString(), slug, secondModel.ID.ValueString())
}
