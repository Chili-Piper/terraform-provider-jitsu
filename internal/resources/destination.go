package resources

import (
	"context"
	"fmt"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &destinationResource{}
	_ resource.ResourceWithImportState = &destinationResource{}
)

type destinationResource struct {
	client *client.Client
}

type destinationModel struct {
	WorkspaceID     types.String `tfsdk:"workspace_id"`
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	DestinationType types.String `tfsdk:"destination_type"`
	Protocol        types.String `tfsdk:"protocol"`
	Hosts           types.List   `tfsdk:"hosts"`
	Username        types.String `tfsdk:"username"`
	Password        types.String `tfsdk:"password"`
	Database        types.String `tfsdk:"database"`
}

func NewDestinationResource() resource.Resource {
	return &destinationResource{}
}

func (r *destinationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_destination"
}

func (r *destinationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Jitsu destination (e.g., ClickHouse, PostgreSQL).",
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
				Description: "Destination ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name of the destination.",
			},
			"destination_type": schema.StringAttribute{
				Required:    true,
				Description: "Destination type (e.g., clickhouse, postgres).",
			},
			"protocol": schema.StringAttribute{
				Optional:    true,
				Description: "Connection protocol (e.g., http, https, tcp).",
			},
			"hosts": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "List of host:port addresses.",
			},
			"username": schema.StringAttribute{
				Optional:    true,
				Description: "Database username.",
			},
			"password": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Database password. API returns masked value; stored in state from user config.",
			},
			"database": schema.StringAttribute{
				Optional:    true,
				Description: "Database name.",
			},
		},
	}
}

func (r *destinationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *destinationResource) buildPayload(ctx context.Context, plan *destinationModel) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"id":              plan.ID.ValueString(),
		"workspaceId":     plan.WorkspaceID.ValueString(),
		"type":            "destination",
		"name":            plan.Name.ValueString(),
		"destinationType": plan.DestinationType.ValueString(),
	}

	if !plan.Protocol.IsNull() && !plan.Protocol.IsUnknown() {
		payload["protocol"] = plan.Protocol.ValueString()
	}

	var hosts []string
	if diags := plan.Hosts.ElementsAs(ctx, &hosts, false); diags.HasError() {
		return nil, fmt.Errorf("reading hosts: %v", diags.Errors())
	}
	payload["hosts"] = hosts

	if !plan.Username.IsNull() && !plan.Username.IsUnknown() {
		payload["username"] = plan.Username.ValueString()
	}
	if !plan.Password.IsNull() && !plan.Password.IsUnknown() {
		payload["password"] = plan.Password.ValueString()
	}
	if !plan.Database.IsNull() && !plan.Database.IsUnknown() {
		payload["database"] = plan.Database.ValueString()
	}

	return payload, nil
}

func (r *destinationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan destinationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload, err := r.buildPayload(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error building payload", err.Error())
		return
	}

	_, err = r.client.Create(ctx, plan.WorkspaceID.ValueString(), "destination", payload)
	if err != nil {
		resp.Diagnostics.AddError("Error creating destination", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *destinationResource) readAPIIntoState(ctx context.Context, result map[string]interface{}, state *destinationModel) diag.Diagnostics {
	var diags diag.Diagnostics

	if v, ok := result["name"].(string); ok {
		state.Name = types.StringValue(v)
	}
	if v, ok := result["destinationType"].(string); ok {
		state.DestinationType = types.StringValue(v)
	}
	if v, ok := result["protocol"].(string); ok {
		state.Protocol = types.StringValue(v)
	} else {
		state.Protocol = types.StringNull()
	}
	if hosts, ok := result["hosts"].([]interface{}); ok {
		hostStrs := make([]string, 0, len(hosts))
		for _, h := range hosts {
			if s, ok := h.(string); ok {
				hostStrs = append(hostStrs, s)
			}
		}
		hostList, d := types.ListValueFrom(ctx, types.StringType, hostStrs)
		diags.Append(d...)
		if d.HasError() {
			state.Hosts = types.ListNull(types.StringType)
		} else {
			state.Hosts = hostList
		}
	} else {
		state.Hosts = types.ListNull(types.StringType)
	}
	if v, ok := result["username"].(string); ok {
		state.Username = types.StringValue(v)
	} else {
		state.Username = types.StringNull()
	}
	// Password: API returns __MASKED_BY_JITSU__ — preserve state value
	if v, ok := result["database"].(string); ok {
		state.Database = types.StringValue(v)
	} else {
		state.Database = types.StringNull()
	}

	return diags
}

func (r *destinationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state destinationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.Read(ctx, state.WorkspaceID.ValueString(), "destination", state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading destination", err.Error())
		return
	}
	if result == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(r.readAPIIntoState(ctx, result, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *destinationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan destinationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload, err := r.buildPayload(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error building payload", err.Error())
		return
	}

	_, err = r.client.Update(ctx, plan.WorkspaceID.ValueString(), "destination", plan.ID.ValueString(), payload)
	if err != nil {
		resp.Diagnostics.AddError("Error updating destination", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *destinationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state destinationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, state.WorkspaceID.ValueString(), "destination", state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Error deleting destination", err.Error())
	}
}

func (r *destinationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := splitImportID(req.ID, 2)
	if parts == nil {
		resp.Diagnostics.AddError("Invalid import ID", "Expected format: workspace_id/destination_id")
		return
	}

	result, err := r.client.Read(ctx, parts[0], "destination", parts[1])
	if err != nil {
		resp.Diagnostics.AddError("Error importing destination", err.Error())
		return
	}
	if result == nil {
		resp.Diagnostics.AddError("Destination not found", fmt.Sprintf("Destination %s not found in workspace %s", parts[1], parts[0]))
		return
	}

	state := destinationModel{
		WorkspaceID: types.StringValue(parts[0]),
		ID:          types.StringValue(parts[1]),
	}
	resp.Diagnostics.Append(r.readAPIIntoState(ctx, result, &state)...)
	// Password not available on import — API returns masked value

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
