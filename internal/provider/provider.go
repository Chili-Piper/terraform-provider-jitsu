package provider

import (
	"context"
	"os"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/chilipiper/terraform-provider-jitsu/internal/resources"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &jitsuProvider{}

type jitsuProvider struct {
	version string
}

type jitsuProviderModel struct {
	ConsoleURL  types.String `tfsdk:"console_url"`
	AuthToken   types.String `tfsdk:"auth_token"`
	DatabaseURL types.String `tfsdk:"database_url"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &jitsuProvider{version: version}
	}
}

func (p *jitsuProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "jitsu"
}

func (p *jitsuProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Jitsu configuration objects (streams, destinations, functions, links).",
		Attributes: map[string]schema.Attribute{
			"console_url": schema.StringAttribute{
				Description: "Jitsu Console URL. Can also be set via JITSU_CONSOLE_URL env var.",
				Optional:    true,
			},
			"auth_token": schema.StringAttribute{
				Description: "Bearer token for Jitsu Console API authentication. " +
					"Can be an admin token or a user API key (format: keyId:secret). " +
					"Can also be set via JITSU_AUTH_TOKEN env var.",
				Optional:  true,
				Sensitive: true,
			},
			"database_url": schema.StringAttribute{
				Description: "PostgreSQL connection string for Console's database. Required to handle destroy+recreate " +
					"(Jitsu uses soft-delete; this allows the provider to hard-delete stale rows). " +
					"Can also be set via JITSU_DATABASE_URL env var.",
				Optional:  true,
				Sensitive: true,
			},
		},
	}
}

func (p *jitsuProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config jitsuProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	consoleURL := os.Getenv("JITSU_CONSOLE_URL")
	if !config.ConsoleURL.IsNull() {
		consoleURL = config.ConsoleURL.ValueString()
	}
	if consoleURL == "" {
		resp.Diagnostics.AddError("Missing console_url", "Set console_url in provider config or JITSU_CONSOLE_URL env var.")
		return
	}

	authToken := os.Getenv("JITSU_AUTH_TOKEN")
	if !config.AuthToken.IsNull() {
		authToken = config.AuthToken.ValueString()
	}
	if authToken == "" {
		resp.Diagnostics.AddError(
			"Missing authentication",
			"Set auth_token in provider config or via JITSU_AUTH_TOKEN env var.",
		)
		return
	}

	databaseURL := os.Getenv("JITSU_DATABASE_URL")
	if !config.DatabaseURL.IsNull() {
		databaseURL = config.DatabaseURL.ValueString()
	}

	userAgent := "terraform-provider-jitsu/" + p.version
	c := client.New(consoleURL, authToken, databaseURL, userAgent)
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *jitsuProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		resources.NewWorkspaceResource,
		resources.NewFunctionResource,
		resources.NewDestinationResource,
		resources.NewStreamResource,
		resources.NewLinkResource,
	}
}

func (p *jitsuProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
