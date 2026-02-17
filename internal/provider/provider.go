package provider

import (
	"context"
	"os"
	"strings"

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
	Username    types.String `tfsdk:"username"`
	Password    types.String `tfsdk:"password"`
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
			"username": schema.StringAttribute{
				Description: "Jitsu username for session authentication. Can also be set via JITSU_USERNAME env var.",
				Optional:    true,
				Sensitive:   true,
			},
			"password": schema.StringAttribute{
				Description: "Jitsu password for session authentication. Can also be set via JITSU_PASSWORD env var.",
				Optional:    true,
				Sensitive:   true,
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

	if !strings.HasPrefix(consoleURL, "https://") {
		resp.Diagnostics.AddWarning(
			"Insecure console_url",
			"console_url does not use HTTPS. Credentials will be sent unencrypted. "+
				"This is acceptable for local development but not recommended for production.",
		)
	}

	username := os.Getenv("JITSU_USERNAME")
	if !config.Username.IsNull() {
		username = config.Username.ValueString()
	}

	password := os.Getenv("JITSU_PASSWORD")
	if !config.Password.IsNull() {
		password = config.Password.ValueString()
	}
	if username == "" || password == "" {
		resp.Diagnostics.AddError(
			"Missing authentication",
			"Set both username/password in provider config or via JITSU_USERNAME/JITSU_PASSWORD.",
		)
		return
	}

	databaseURL := os.Getenv("JITSU_DATABASE_URL")
	if !config.DatabaseURL.IsNull() {
		databaseURL = config.DatabaseURL.ValueString()
	}

	userAgent := "terraform-provider-jitsu/" + p.version
	c := client.New(consoleURL, username, password, databaseURL, userAgent)
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
