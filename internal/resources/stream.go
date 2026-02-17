package resources

import (
	"context"
	"fmt"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &streamResource{}
	_ resource.ResourceWithImportState = &streamResource{}
)

type streamResource struct {
	client *client.Client
}

type streamKeyModel struct {
	ID        types.String `tfsdk:"id"`
	Plaintext types.String `tfsdk:"plaintext"`
}

var streamKeyAttrTypes = map[string]attr.Type{
	"id":        types.StringType,
	"plaintext": types.StringType,
}

type streamModel struct {
	WorkspaceID types.String `tfsdk:"workspace_id"`
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	PublicKeys  types.List   `tfsdk:"public_keys"`
	PrivateKeys types.List   `tfsdk:"private_keys"`
}

func NewStreamResource() resource.Resource {
	return &streamResource{}
}

func (r *streamResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_stream"
}

func (r *streamResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keySchema := schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Key identifier.",
			},
			"plaintext": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "Plaintext key value. Write-only — API returns hashed value on read.",
			},
		},
	}

	resp.Schema = schema.Schema{
		Description: "Manages a Jitsu stream (event source). Keys are set via a two-step create (POST) then update (PUT).",
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
				Description: "Stream ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name of the stream.",
			},
			"public_keys": schema.ListNestedAttribute{
				Optional:     true,
				Description:  "Public (browser) write keys.",
				NestedObject: keySchema,
			},
			"private_keys": schema.ListNestedAttribute{
				Optional:     true,
				Description:  "Private (server-to-server) write keys.",
				NestedObject: keySchema,
			},
		},
	}
}

func (r *streamResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func keysToPayload(ctx context.Context, keys types.List) ([]map[string]string, error) {
	if keys.IsNull() || keys.IsUnknown() || len(keys.Elements()) == 0 {
		return nil, nil
	}
	var models []streamKeyModel
	if diags := keys.ElementsAs(ctx, &models, false); diags.HasError() {
		return nil, fmt.Errorf("reading keys: %v", diags.Errors())
	}
	result := make([]map[string]string, len(models))
	for i, m := range models {
		result[i] = map[string]string{
			"id":        m.ID.ValueString(),
			"plaintext": m.Plaintext.ValueString(),
		}
	}
	return result, nil
}

func (r *streamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan streamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Precompute key payloads before creation so conversion errors don't leave orphaned streams.
	pubKeys, err := keysToPayload(ctx, plan.PublicKeys)
	if err != nil {
		resp.Diagnostics.AddError("Error building public keys", err.Error())
		return
	}
	privKeys, err := keysToPayload(ctx, plan.PrivateKeys)
	if err != nil {
		resp.Diagnostics.AddError("Error building private keys", err.Error())
		return
	}
	hasKeys := pubKeys != nil || privKeys != nil

	// Step 1: POST creates stream without keys
	tflog.Debug(ctx, "creating stream (step 1: POST without keys)", map[string]interface{}{
		"id": plan.ID.ValueString(),
	})
	createPayload := map[string]interface{}{
		"id":          plan.ID.ValueString(),
		"workspaceId": plan.WorkspaceID.ValueString(),
		"type":        "stream",
		"name":        plan.Name.ValueString(),
	}

	_, err = r.client.Create(ctx, plan.WorkspaceID.ValueString(), "stream", createPayload)
	if err != nil {
		resp.Diagnostics.AddError("Error creating stream", err.Error())
		return
	}

	// Step 2: PUT sets keys via plaintext field
	if hasKeys {
		tflog.Debug(ctx, "setting stream keys (step 2: PUT with plaintext keys)", map[string]interface{}{
			"id": plan.ID.ValueString(),
		})
		updatePayload := map[string]interface{}{
			"id":          plan.ID.ValueString(),
			"workspaceId": plan.WorkspaceID.ValueString(),
			"type":        "stream",
			"name":        plan.Name.ValueString(),
		}

		if pubKeys != nil {
			updatePayload["publicKeys"] = pubKeys
		}

		if privKeys != nil {
			updatePayload["privateKeys"] = privKeys
		}

		_, err = r.client.Update(ctx, plan.WorkspaceID.ValueString(), "stream", plan.ID.ValueString(), updatePayload)
		if err != nil {
			rollbackErr := r.client.Delete(ctx, plan.WorkspaceID.ValueString(), "stream", plan.ID.ValueString())
			if rollbackErr != nil {
				resp.Diagnostics.AddError(
					"Error setting stream keys",
					fmt.Sprintf("%s. Rollback failed for stream %q: %s", err.Error(), plan.ID.ValueString(), rollbackErr.Error()),
				)
				return
			}
			resp.Diagnostics.AddError(
				"Error setting stream keys",
				fmt.Sprintf("%s. Rolled back newly-created stream %q.", err.Error(), plan.ID.ValueString()),
			)
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *streamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state streamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := r.client.Read(ctx, state.WorkspaceID.ValueString(), "stream", state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading stream", err.Error())
		return
	}
	if result == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	if v, ok := result["name"].(string); ok {
		state.Name = types.StringValue(v)
	}
	// Keys: API returns hashed values, not plaintext. Preserve state values.

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *streamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan streamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload := map[string]interface{}{
		"id":          plan.ID.ValueString(),
		"workspaceId": plan.WorkspaceID.ValueString(),
		"type":        "stream",
		"name":        plan.Name.ValueString(),
	}

	pubKeys, err := keysToPayload(ctx, plan.PublicKeys)
	if err != nil {
		resp.Diagnostics.AddError("Error building public keys", err.Error())
		return
	}
	if pubKeys != nil {
		payload["publicKeys"] = pubKeys
	}

	privKeys, err := keysToPayload(ctx, plan.PrivateKeys)
	if err != nil {
		resp.Diagnostics.AddError("Error building private keys", err.Error())
		return
	}
	if privKeys != nil {
		payload["privateKeys"] = privKeys
	}

	_, err = r.client.Update(ctx, plan.WorkspaceID.ValueString(), "stream", plan.ID.ValueString(), payload)
	if err != nil {
		resp.Diagnostics.AddError("Error updating stream", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *streamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state streamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, state.WorkspaceID.ValueString(), "stream", state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Error deleting stream", err.Error())
	}
}

func (r *streamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := splitImportID(req.ID, 2)
	if parts == nil {
		resp.Diagnostics.AddError("Invalid import ID", "Expected format: workspace_id/stream_id")
		return
	}

	result, err := r.client.Read(ctx, parts[0], "stream", parts[1])
	if err != nil {
		resp.Diagnostics.AddError("Error importing stream", err.Error())
		return
	}
	if result == nil {
		resp.Diagnostics.AddError("Stream not found", fmt.Sprintf("Stream %s not found in workspace %s", parts[1], parts[0]))
		return
	}

	state := streamModel{
		WorkspaceID: types.StringValue(parts[0]),
		ID:          types.StringValue(parts[1]),
	}
	if v, ok := result["name"].(string); ok {
		state.Name = types.StringValue(v)
	}
	// Keys not available on import — API returns hashed values
	state.PublicKeys = types.ListNull(types.ObjectType{AttrTypes: streamKeyAttrTypes})
	state.PrivateKeys = types.ListNull(types.ObjectType{AttrTypes: streamKeyAttrTypes})

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
