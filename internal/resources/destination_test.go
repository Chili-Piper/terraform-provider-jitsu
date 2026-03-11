package resources

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDestinationReadAPIIntoState_ClearsAbsentOptionalFields(t *testing.T) {
	ctx := context.Background()

	existingHosts, diags := types.ListValueFrom(ctx, types.StringType, []string{"old-host:8123"})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building existing hosts: %v", diags)
	}

	state := destinationModel{
		ClickHouse: &clickhouseModel{
			Protocol: types.StringValue("http"),
			Username: types.StringValue("reporting"),
			Database: types.StringValue("default"),
			Cluster:  types.StringValue("default"),
			Hosts:    existingHosts,
		},
	}

	result := map[string]interface{}{
		"name":            "Updated Destination",
		"destinationType": "clickhouse",
		"hosts":           []interface{}{"new-host:8123"},
	}

	diags = (&destinationResource{}).readAPIIntoState(ctx, result, &state)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	ch := state.ClickHouse
	if ch == nil {
		t.Fatal("clickhouse block should be set")
	}
	if !ch.Protocol.IsNull() {
		t.Fatalf("protocol should be null, got %v", ch.Protocol)
	}
	if !ch.Username.IsNull() {
		t.Fatalf("username should be null, got %v", ch.Username)
	}
	if !ch.Database.IsNull() {
		t.Fatalf("database should be null, got %v", ch.Database)
	}
	if !ch.Cluster.IsNull() {
		t.Fatalf("cluster should be null, got %v", ch.Cluster)
	}

	var hosts []string
	diags = ch.Hosts.ElementsAs(ctx, &hosts, false)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics reading hosts: %v", diags)
	}
	if !reflect.DeepEqual(hosts, []string{"new-host:8123"}) {
		t.Fatalf("hosts mismatch: got %v", hosts)
	}

	if state.BigQuery != nil {
		t.Fatal("bigquery block should be nil for clickhouse destination")
	}
}

func TestDestinationReadAPIIntoState_NullsHostsWhenMissing(t *testing.T) {
	ctx := context.Background()

	existingHosts, diags := types.ListValueFrom(ctx, types.StringType, []string{"old-host:8123"})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building existing hosts: %v", diags)
	}

	state := destinationModel{
		ClickHouse: &clickhouseModel{
			Hosts: existingHosts,
		},
	}

	result := map[string]interface{}{
		"name":            "Updated Destination",
		"destinationType": "clickhouse",
	}

	diags = (&destinationResource{}).readAPIIntoState(ctx, result, &state)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if state.ClickHouse.Hosts.IsNull() != true {
		t.Fatalf("hosts should be null when API does not return hosts, got %v", state.ClickHouse.Hosts)
	}
}

func TestDestinationReadAPIIntoState_BigQuery(t *testing.T) {
	ctx := context.Background()

	state := destinationModel{
		BigQuery: &bigqueryModel{
			Credentials: types.StringValue("secret-json-key"),
		},
	}

	result := map[string]interface{}{
		"name":            "BQ Destination",
		"destinationType": "bigquery",
		"projectId":       "my-project",
		"bqDataset":       "my_dataset",
		"credentials":     "__MASKED_BY_JITSU__",
	}

	diags := (&destinationResource{}).readAPIIntoState(ctx, result, &state)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	bq := state.BigQuery
	if bq == nil {
		t.Fatal("bigquery block should be set")
	}
	if bq.ProjectID.ValueString() != "my-project" {
		t.Fatalf("project_id mismatch: got %q", bq.ProjectID.ValueString())
	}
	if bq.BQDataset.ValueString() != "my_dataset" {
		t.Fatalf("bq_dataset mismatch: got %q", bq.BQDataset.ValueString())
	}
	// Credentials should be preserved from state, not overwritten with masked value
	if bq.Credentials.ValueString() != "secret-json-key" {
		t.Fatalf("credentials should be preserved from state, got %q", bq.Credentials.ValueString())
	}

	if state.ClickHouse != nil {
		t.Fatal("clickhouse block should be nil for bigquery destination")
	}
}

func TestDestinationBuildPayload_RejectsInvalidBlockCombinations(t *testing.T) {
	ctx := context.Background()

	clickhouseHosts, diags := types.ListValueFrom(ctx, types.StringType, []string{"clickhouse:8123"})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building clickhouse hosts: %v", diags)
	}

	tests := []struct {
		name    string
		plan    destinationModel
		wantErr string
	}{
		{
			name: "bigquery requires bigquery block",
			plan: destinationModel{
				WorkspaceID:     types.StringValue("workspace-id"),
				ID:              types.StringValue("destination-id"),
				Name:            types.StringValue("Destination"),
				DestinationType: types.StringValue("bigquery"),
			},
			wantErr: "BigQuery destinations must define the bigquery block.",
		},
		{
			name: "clickhouse destinations require clickhouse block",
			plan: destinationModel{
				WorkspaceID:     types.StringValue("workspace-id"),
				ID:              types.StringValue("destination-id"),
				Name:            types.StringValue("Destination"),
				DestinationType: types.StringValue("clickhouse"),
			},
			wantErr: "\"clickhouse\" destinations must define the clickhouse block.",
		},
		{
			name: "bigquery destinations reject clickhouse block",
			plan: destinationModel{
				WorkspaceID:     types.StringValue("workspace-id"),
				ID:              types.StringValue("destination-id"),
				Name:            types.StringValue("Destination"),
				DestinationType: types.StringValue("bigquery"),
				ClickHouse: &clickhouseModel{
					Hosts: clickhouseHosts,
				},
			},
			wantErr: "BigQuery destinations cannot define the clickhouse block.",
		},
		{
			name: "clickhouse destinations reject bigquery block",
			plan: destinationModel{
				WorkspaceID:     types.StringValue("workspace-id"),
				ID:              types.StringValue("destination-id"),
				Name:            types.StringValue("Destination"),
				DestinationType: types.StringValue("clickhouse"),
				BigQuery: &bigqueryModel{
					Credentials: types.StringValue("credentials"),
					ProjectID:   types.StringValue("project-id"),
					BQDataset:   types.StringValue("dataset"),
				},
			},
			wantErr: "\"clickhouse\" destinations cannot define the bigquery block.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := (&destinationResource{}).buildPayload(ctx, &tt.plan)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error to contain %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}
