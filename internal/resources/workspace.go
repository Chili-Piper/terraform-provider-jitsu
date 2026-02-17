package resources

import (
	"context"
	"fmt"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &workspaceResource{}
	_ resource.ResourceWithImportState = &workspaceResource{}
)

type workspaceResource struct {
	client *client.Client
}

type workspaceModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Slug types.String `tfsdk:"slug"`
}

func NewWorkspaceResource() resource.Resource {
	return &workspaceResource{}
}

func (r *workspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (r *workspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Jitsu workspace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace ID (assigned by Console).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Workspace display name.",
			},
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "Workspace slug.",
			},
		},
	}
}

func (r *workspaceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *workspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.WorkspaceCreate(ctx, plan.Name.ValueString(), plan.Slug.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating workspace", err.Error())
		return
	}

	// Jitsu Console may accept slug on create but persist it as null.
	// Force slug persistence by issuing an immediate update with the same values.
	_, err = r.client.WorkspaceUpdate(ctx, id, plan.Name.ValueString(), plan.Slug.ValueString())
	if err != nil {
		rollbackErr := r.client.WorkspaceDelete(ctx, id)
		if rollbackErr != nil {
			resp.Diagnostics.AddError(
				"Error finalizing workspace creation",
				fmt.Sprintf("%s. Rollback failed for workspace %q: %s", err.Error(), id, rollbackErr.Error()),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Error finalizing workspace creation",
			fmt.Sprintf("%s. Rolled back newly-created workspace %q.", err.Error(), id),
		)
		return
	}

	state := plan
	state.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *workspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.WorkspaceRead(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading workspace", err.Error())
		return
	}
	if result == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	if v, ok := result["id"].(string); ok {
		state.ID = types.StringValue(v)
	}
	if v, ok := result["name"].(string); ok {
		state.Name = types.StringValue(v)
	}
	if v, ok := result["slug"].(string); ok {
		state.Slug = types.StringValue(v)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *workspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.WorkspaceUpdate(ctx, state.ID.ValueString(), plan.Name.ValueString(), plan.Slug.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error updating workspace", err.Error())
		return
	}

	newState := plan
	if v, ok := result["id"].(string); ok && v != "" {
		newState.ID = types.StringValue(v)
	} else {
		newState.ID = state.ID
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *workspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.WorkspaceDelete(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Error deleting workspace", err.Error())
	}
}

func (r *workspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := splitImportID(req.ID, 1)
	if parts == nil {
		resp.Diagnostics.AddError("Invalid import ID", "Expected format: workspace_id_or_slug")
		return
	}

	result, err := r.client.WorkspaceRead(ctx, parts[0])
	if err != nil {
		resp.Diagnostics.AddError("Error importing workspace", err.Error())
		return
	}
	if result == nil {
		resp.Diagnostics.AddError("Workspace not found", fmt.Sprintf("Workspace %s not found", parts[0]))
		return
	}

	state := workspaceModel{
		ID: types.StringValue(parts[0]),
	}
	if v, ok := result["id"].(string); ok && v != "" {
		state.ID = types.StringValue(v)
	}
	if v, ok := result["name"].(string); ok {
		state.Name = types.StringValue(v)
	}
	if v, ok := result["slug"].(string); ok {
		state.Slug = types.StringValue(v)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
