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
	_ resource.Resource                = &functionResource{}
	_ resource.ResourceWithImportState = &functionResource{}
)

type functionResource struct {
	client *client.Client
}

type functionModel struct {
	WorkspaceID types.String `tfsdk:"workspace_id"`
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Code        types.String `tfsdk:"code"`
}

func NewFunctionResource() resource.Resource {
	return &functionResource{}
}

func (r *functionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_function"
}

func (r *functionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Jitsu function. The ID must be a valid JS identifier (use underscores, not hyphens).",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Required:    true,
				Description: "Jitsu workspace ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Function ID. Must be a valid JS identifier (no hyphens).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name of the function.",
			},
			"code": schema.StringAttribute{
				Required:    true,
				Description: "JavaScript function code.",
			},
		},
	}
}

func (r *functionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *functionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan functionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload := map[string]interface{}{
		"id":          plan.ID.ValueString(),
		"workspaceId": plan.WorkspaceID.ValueString(),
		"type":        "function",
		"name":        plan.Name.ValueString(),
		"code":        plan.Code.ValueString(),
	}

	_, err := r.client.Create(ctx, plan.WorkspaceID.ValueString(), "function", payload)
	if err != nil {
		resp.Diagnostics.AddError("Error creating function", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *functionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state functionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.Read(ctx, state.WorkspaceID.ValueString(), "function", state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading function", err.Error())
		return
	}
	if result == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	if v, ok := result["name"].(string); ok {
		state.Name = types.StringValue(v)
	}
	if v, ok := result["code"].(string); ok {
		state.Code = types.StringValue(v)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *functionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan functionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload := map[string]interface{}{
		"id":          plan.ID.ValueString(),
		"workspaceId": plan.WorkspaceID.ValueString(),
		"type":        "function",
		"name":        plan.Name.ValueString(),
		"code":        plan.Code.ValueString(),
	}

	_, err := r.client.Update(ctx, plan.WorkspaceID.ValueString(), "function", plan.ID.ValueString(), payload)
	if err != nil {
		resp.Diagnostics.AddError("Error updating function", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *functionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state functionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, state.WorkspaceID.ValueString(), "function", state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Error deleting function", err.Error())
	}
}

func (r *functionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := splitImportID(req.ID, 2)
	if parts == nil {
		resp.Diagnostics.AddError("Invalid import ID", "Expected format: workspace_id/function_id")
		return
	}

	result, err := r.client.Read(ctx, parts[0], "function", parts[1])
	if err != nil {
		resp.Diagnostics.AddError("Error importing function", err.Error())
		return
	}
	if result == nil {
		resp.Diagnostics.AddError("Function not found", fmt.Sprintf("Function %s not found in workspace %s", parts[1], parts[0]))
		return
	}

	state := functionModel{
		WorkspaceID: types.StringValue(parts[0]),
		ID:          types.StringValue(parts[1]),
	}
	if v, ok := result["name"].(string); ok {
		state.Name = types.StringValue(v)
	}
	if v, ok := result["code"].(string); ok {
		state.Code = types.StringValue(v)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
