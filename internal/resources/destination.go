package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                   = &destinationResource{}
	_ resource.ResourceWithImportState    = &destinationResource{}
	_ resource.ResourceWithValidateConfig = &destinationResource{}
)

type destinationResource struct {
	client *client.Client
}

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

type destinationModel struct {
	WorkspaceID     types.String     `tfsdk:"workspace_id"`
	ID              types.String     `tfsdk:"id"`
	Name            types.String     `tfsdk:"name"`
	DestinationType types.String     `tfsdk:"destination_type"`
	ClickHouse      *clickhouseModel `tfsdk:"clickhouse"`
	BigQuery        *bigqueryModel   `tfsdk:"bigquery"`
}

type destinationConfigIssue struct {
	attribute string
	detail    string
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

	for _, issue := range destinationConfigIssues(&config) {
		resp.Diagnostics.AddAttributeError(
			path.Root(issue.attribute),
			"Invalid destination configuration",
			issue.detail,
		)
	}
}

func destinationConfigIssues(config *destinationModel) []destinationConfigIssue {
	if config == nil {
		return nil
	}

	hasClickHouse := config.ClickHouse != nil
	hasBigQuery := config.BigQuery != nil

	if config.DestinationType.IsNull() || config.DestinationType.IsUnknown() {
		if hasClickHouse && hasBigQuery {
			return []destinationConfigIssue{
				{
					attribute: "bigquery",
					detail:    "Only one destination config block may be set. Choose either clickhouse or bigquery to match destination_type.",
				},
			}
		}
		return nil
	}

	isBigQuery := config.DestinationType.ValueString() == "bigquery"
	issues := make([]destinationConfigIssue, 0, 2)

	if isBigQuery {
		if !hasBigQuery {
			issues = append(issues, destinationConfigIssue{
				attribute: "bigquery",
				detail:    "BigQuery destinations must define the bigquery block.",
			})
		}
		if hasClickHouse {
			issues = append(issues, destinationConfigIssue{
				attribute: "clickhouse",
				detail:    "BigQuery destinations cannot define the clickhouse block.",
			})
		}
		return issues
	}

	if !hasClickHouse {
		issues = append(issues, destinationConfigIssue{
			attribute: "clickhouse",
			detail:    fmt.Sprintf("%q destinations must define the clickhouse block.", config.DestinationType.ValueString()),
		})
	}
	if hasBigQuery {
		issues = append(issues, destinationConfigIssue{
			attribute: "bigquery",
			detail:    fmt.Sprintf("%q destinations cannot define the bigquery block.", config.DestinationType.ValueString()),
		})
	}

	return issues
}

func (r *destinationResource) buildPayload(ctx context.Context, plan *destinationModel) (map[string]interface{}, error) {
	if issues := destinationConfigIssues(plan); len(issues) > 0 {
		details := make([]string, 0, len(issues))
		for _, issue := range issues {
			details = append(details, issue.detail)
		}
		return nil, fmt.Errorf("invalid destination configuration: %s", strings.Join(details, " "))
	}

	payload := map[string]interface{}{
		"id":              plan.ID.ValueString(),
		"workspaceId":     plan.WorkspaceID.ValueString(),
		"type":            "destination",
		"name":            plan.Name.ValueString(),
		"destinationType": plan.DestinationType.ValueString(),
	}

	if ch := plan.ClickHouse; ch != nil {
		if !ch.Protocol.IsNull() && !ch.Protocol.IsUnknown() {
			payload["protocol"] = ch.Protocol.ValueString()
		}
		var hosts []string
		if diags := ch.Hosts.ElementsAs(ctx, &hosts, false); diags.HasError() {
			return nil, fmt.Errorf("reading hosts: %v", diags.Errors())
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

	if bq := plan.BigQuery; bq != nil {
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
		// Credentials: API returns masked value — preserve state value
		if state.BigQuery != nil {
			bq.Credentials = state.BigQuery.Credentials
		}
		if v, ok := result["projectId"].(string); ok {
			bq.ProjectID = types.StringValue(v)
		}
		if v, ok := result["bqDataset"].(string); ok {
			bq.BQDataset = types.StringValue(v)
		}
		state.BigQuery = bq
		state.ClickHouse = nil

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
		// Password: API returns masked value — preserve state value
		if state.ClickHouse != nil {
			ch.Password = state.ClickHouse.Password
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
		state.ClickHouse = ch
		state.BigQuery = nil
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
	// Password/credentials not available on import — API returns masked values

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
