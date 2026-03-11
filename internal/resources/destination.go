package resources

import (
	"context"
	"fmt"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var (
	_ resource.Resource                   = &destinationResource{}
	_ resource.ResourceWithImportState    = &destinationResource{}
	_ resource.ResourceWithValidateConfig = &destinationResource{}
)

type destinationResource struct {
	client *client.Client
}

// Typed models for extracting nested object values via As().
type clickhouseModel struct {
	Protocol types.String `tfsdk:"protocol"`
	Hosts    types.List   `tfsdk:"hosts"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
	Database types.String `tfsdk:"database"`
	Cluster  types.String `tfsdk:"cluster"`
}

type bigqueryModel struct {
	Credentials types.String `tfsdk:"credentials"`
	ProjectID   types.String `tfsdk:"project_id"`
	BQDataset   types.String `tfsdk:"bq_dataset"`
}

// Attribute type maps for constructing types.Object values.
var clickhouseAttrTypes = map[string]attr.Type{
	"protocol": types.StringType,
	"hosts":    types.ListType{ElemType: types.StringType},
	"username": types.StringType,
	"password": types.StringType,
	"database": types.StringType,
	"cluster":  types.StringType,
}

var bigqueryAttrTypes = map[string]attr.Type{
	"credentials": types.StringType,
	"project_id":  types.StringType,
	"bq_dataset":  types.StringType,
}

// destinationModel uses types.Object for nested attributes so the framework
// can handle null, unknown, and concrete values at all lifecycle stages
// (validate, plan, apply). Use clickhouse() / bigquery() to extract the
// typed models when values are known.
type destinationModel struct {
	WorkspaceID     types.String `tfsdk:"workspace_id"`
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	DestinationType types.String `tfsdk:"destination_type"`
	ClickHouse      types.Object `tfsdk:"clickhouse"`
	BigQuery        types.Object `tfsdk:"bigquery"`
}

func (m *destinationModel) clickhouse(ctx context.Context) (*clickhouseModel, diag.Diagnostics) {
	if m.ClickHouse.IsNull() || m.ClickHouse.IsUnknown() {
		return nil, nil
	}
	var ch clickhouseModel
	diags := m.ClickHouse.As(ctx, &ch, basetypes.ObjectAsOptions{})
	return &ch, diags
}

func (m *destinationModel) bigquery(ctx context.Context) (*bigqueryModel, diag.Diagnostics) {
	if m.BigQuery.IsNull() || m.BigQuery.IsUnknown() {
		return nil, nil
	}
	var bq bigqueryModel
	diags := m.BigQuery.As(ctx, &bq, basetypes.ObjectAsOptions{})
	return &bq, diags
}

func NewDestinationResource() resource.Resource {
	return &destinationResource{}
}

func (r *destinationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_destination"
}

func (r *destinationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Jitsu destination (e.g., ClickHouse, BigQuery).",
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
				Description: "Destination type (e.g., clickhouse, bigquery).",
			},
			"clickhouse": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "ClickHouse destination configuration.",
				Attributes: map[string]schema.Attribute{
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
					"cluster": schema.StringAttribute{
						Optional:    true,
						Description: "ClickHouse cluster name. When set, Bulker creates tables with Replicated* engines for cross-replica data replication.",
					},
				},
			},
			"bigquery": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "BigQuery destination configuration.",
				Attributes: map[string]schema.Attribute{
					"credentials": schema.StringAttribute{
						Required:    true,
						Sensitive:   true,
						Description: "BigQuery service account JSON key.",
					},
					"project_id": schema.StringAttribute{
						Required:    true,
						Description: "GCP project ID.",
					},
					"bq_dataset": schema.StringAttribute{
						Required:    true,
						Description: "BigQuery dataset name.",
					},
				},
			},
		},
	}
}

func (r *destinationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *destinationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config destinationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// For each nested block, determine whether it is definitively set,
	// definitively absent (null), or unknown. We only skip individual
	// checks that depend on an unknown value — everything else is still
	// validated at plan time.
	chSet := !config.ClickHouse.IsNull() && !config.ClickHouse.IsUnknown()
	chNull := config.ClickHouse.IsNull()
	bqSet := !config.BigQuery.IsNull() && !config.BigQuery.IsUnknown()
	bqNull := config.BigQuery.IsNull()

	// Both blocks set is always invalid regardless of destination_type.
	if chSet && bqSet {
		resp.Diagnostics.AddAttributeError(
			path.Root("bigquery"),
			"Invalid destination configuration",
			"Only one destination config block may be set. Choose either clickhouse or bigquery to match destination_type.",
		)
	}

	// Without a known destination_type we can't do type-specific checks.
	if config.DestinationType.IsNull() || config.DestinationType.IsUnknown() {
		return
	}

	isBigQuery := config.DestinationType.ValueString() == "bigquery"

	if isBigQuery {
		// Missing bigquery block — flag when definitively null (not unknown).
		if bqNull {
			resp.Diagnostics.AddAttributeError(
				path.Root("bigquery"),
				"Invalid destination configuration",
				"BigQuery destinations must define the bigquery block.",
			)
		}
		// Extra clickhouse block — flag when definitively set (not unknown).
		if chSet {
			resp.Diagnostics.AddAttributeError(
				path.Root("clickhouse"),
				"Invalid destination configuration",
				"BigQuery destinations cannot define the clickhouse block.",
			)
		}
		return
	}

	// Non-bigquery: missing clickhouse block — flag when definitively null.
	if chNull {
		resp.Diagnostics.AddAttributeError(
			path.Root("clickhouse"),
			"Invalid destination configuration",
			fmt.Sprintf("%q destinations must define the clickhouse block.", config.DestinationType.ValueString()),
		)
	}
	// Non-bigquery: extra bigquery block — flag when definitively set.
	if bqSet {
		resp.Diagnostics.AddAttributeError(
			path.Root("bigquery"),
			"Invalid destination configuration",
			fmt.Sprintf("%q destinations cannot define the bigquery block.", config.DestinationType.ValueString()),
		)
	}
}

func (r *destinationResource) buildPayload(ctx context.Context, plan *destinationModel) (map[string]interface{}, error) {
	// Defense-in-depth: ValidateConfig may have skipped checks when values
	// were unknown. At plan/apply time all values are concrete, so validate
	// the block/type combination here as a safety net.
	ch, diags := plan.clickhouse(ctx)
	if diags.HasError() {
		return nil, fmt.Errorf("reading clickhouse config: %v", diags.Errors())
	}
	bq, diags := plan.bigquery(ctx)
	if diags.HasError() {
		return nil, fmt.Errorf("reading bigquery config: %v", diags.Errors())
	}
	isBigQuery := plan.DestinationType.ValueString() == "bigquery"
	if isBigQuery && bq == nil {
		return nil, fmt.Errorf("BigQuery destinations must define the bigquery block")
	}
	if isBigQuery && ch != nil {
		return nil, fmt.Errorf("BigQuery destinations cannot define the clickhouse block")
	}
	if !isBigQuery && ch == nil {
		return nil, fmt.Errorf("%q destinations must define the clickhouse block", plan.DestinationType.ValueString())
	}
	if !isBigQuery && bq != nil {
		return nil, fmt.Errorf("%q destinations cannot define the bigquery block", plan.DestinationType.ValueString())
	}

	payload := map[string]interface{}{
		"id":              plan.ID.ValueString(),
		"workspaceId":     plan.WorkspaceID.ValueString(),
		"type":            "destination",
		"name":            plan.Name.ValueString(),
		"destinationType": plan.DestinationType.ValueString(),
	}

	if ch != nil {
		if !ch.Protocol.IsNull() && !ch.Protocol.IsUnknown() {
			payload["protocol"] = ch.Protocol.ValueString()
		}
		var hosts []string
		if d := ch.Hosts.ElementsAs(ctx, &hosts, false); d.HasError() {
			return nil, fmt.Errorf("reading hosts: %v", d.Errors())
		}
		payload["hosts"] = hosts
		if !ch.Username.IsNull() && !ch.Username.IsUnknown() {
			payload["username"] = ch.Username.ValueString()
		}
		if !ch.Password.IsNull() && !ch.Password.IsUnknown() {
			payload["password"] = ch.Password.ValueString()
		}
		if !ch.Database.IsNull() && !ch.Database.IsUnknown() {
			payload["database"] = ch.Database.ValueString()
		}
		if !ch.Cluster.IsNull() && !ch.Cluster.IsUnknown() {
			payload["cluster"] = ch.Cluster.ValueString()
		}
	}

	if bq != nil {
		payload["credentials"] = bq.Credentials.ValueString()
		payload["projectId"] = bq.ProjectID.ValueString()
		payload["bqDataset"] = bq.BQDataset.ValueString()
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

	destType, _ := result["destinationType"].(string)

	switch destType {
	case "bigquery":
		bq := &bigqueryModel{}
		// Credentials: API returns masked value — preserve state value.
		oldBQ, d := state.bigquery(ctx)
		diags.Append(d...)
		if oldBQ != nil {
			bq.Credentials = oldBQ.Credentials
		}
		if v, ok := result["projectId"].(string); ok {
			bq.ProjectID = types.StringValue(v)
		}
		if v, ok := result["bqDataset"].(string); ok {
			bq.BQDataset = types.StringValue(v)
		}
		objVal, d := types.ObjectValueFrom(ctx, bigqueryAttrTypes, bq)
		diags.Append(d...)
		state.BigQuery = objVal
		state.ClickHouse = types.ObjectNull(clickhouseAttrTypes)

	default:
		ch := &clickhouseModel{}
		if v, ok := result["protocol"].(string); ok {
			ch.Protocol = types.StringValue(v)
		} else {
			ch.Protocol = types.StringNull()
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
				ch.Hosts = types.ListNull(types.StringType)
			} else {
				ch.Hosts = hostList
			}
		} else {
			ch.Hosts = types.ListNull(types.StringType)
		}
		if v, ok := result["username"].(string); ok {
			ch.Username = types.StringValue(v)
		} else {
			ch.Username = types.StringNull()
		}
		// Password: API returns masked value — preserve state value.
		oldCH, d := state.clickhouse(ctx)
		diags.Append(d...)
		if oldCH != nil {
			ch.Password = oldCH.Password
		}
		if v, ok := result["database"].(string); ok {
			ch.Database = types.StringValue(v)
		} else {
			ch.Database = types.StringNull()
		}
		if v, ok := result["cluster"].(string); ok {
			ch.Cluster = types.StringValue(v)
		} else {
			ch.Cluster = types.StringNull()
		}
		objVal, d := types.ObjectValueFrom(ctx, clickhouseAttrTypes, ch)
		diags.Append(d...)
		state.ClickHouse = objVal
		state.BigQuery = types.ObjectNull(bigqueryAttrTypes)
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
		ClickHouse:  types.ObjectNull(clickhouseAttrTypes),
		BigQuery:    types.ObjectNull(bigqueryAttrTypes),
	}
	resp.Diagnostics.Append(r.readAPIIntoState(ctx, result, &state)...)
	// Password/credentials not available on import — API returns masked values

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
