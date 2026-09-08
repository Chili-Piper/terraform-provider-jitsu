package resources

import (
	"context"
	"fmt"
	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"os"
	"strings"
	"testing"
	"time"
)

func auditConsole(t *testing.T) (*client.Client, string) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	ctx := context.Background()
	c := client.New(os.Getenv("JITSU_CONSOLE_URL"), os.Getenv("JITSU_AUTH_TOKEN"), os.Getenv("JITSU_DATABASE_URL"), "audit-regression")
	ws, err := c.WorkspaceCreate(ctx, "Audit regression", fmt.Sprintf("audit-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.WorkspaceDelete(ctx, ws); err != nil {
			t.Error(err)
		}
		c.Close()
	})
	return c, ws
}

func TestAccAuditConsoleErrorSecret(t *testing.T) {
	c, ws := auditConsole(t)
	ctx := context.Background()
	r := &destinationResource{client: c}
	hosts, d := types.ListValueFrom(ctx, types.StringType, []string{"clickhouse:8123"})
	if d.HasError() {
		t.Fatal(d)
	}
	model := destinationModel{WorkspaceID: types.StringValue(ws), ID: types.StringValue("invalid_" + ws), Name: types.StringValue("Invalid protocol"), DestinationType: types.StringValue("clickhouse"), BigQuery: types.ObjectNull(bigqueryAttrTypes), ClickHouse: mustClickhouseObject(t, ctx, &clickhouseModel{Protocol: types.StringValue("tcp"), Hosts: hosts, Username: types.StringValue("reporting"), Password: types.StringValue("credential-canary"), Database: types.StringValue("default"), Cluster: types.StringValue("")})}
	planned := createRollbackTestState(t, r, &model)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: planned.Schema}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Raw: planned.Raw, Schema: planned.Schema}}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("invalid protocol unexpectedly accepted")
	}
	for _, diagnostic := range resp.Diagnostics {
		if strings.Contains(diagnostic.Detail(), "credential-canary") {
			t.Fatal("Console validation response leaked password through Terraform diagnostic")
		}
	}
}
