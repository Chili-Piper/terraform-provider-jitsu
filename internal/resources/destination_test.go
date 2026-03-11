package resources

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func mustClickhouseObject(t *testing.T, ctx context.Context, ch *clickhouseModel) types.Object {
	t.Helper()
	obj, diags := types.ObjectValueFrom(ctx, clickhouseAttrTypes, ch)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building clickhouse object: %v", diags)
	}
	return obj
}

func mustBigqueryObject(t *testing.T, ctx context.Context, bq *bigqueryModel) types.Object {
	t.Helper()
	obj, diags := types.ObjectValueFrom(ctx, bigqueryAttrTypes, bq)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building bigquery object: %v", diags)
	}
	return obj
}

func TestDestinationReadAPIIntoState_ClearsAbsentOptionalFields(t *testing.T) {
	ctx := context.Background()

	existingHosts, diags := types.ListValueFrom(ctx, types.StringType, []string{"old-host:8123"})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building existing hosts: %v", diags)
	}

	state := destinationModel{
		ClickHouse: mustClickhouseObject(t, ctx, &clickhouseModel{
			Protocol: types.StringValue("http"),
			Username: types.StringValue("reporting"),
			Database: types.StringValue("default"),
			Cluster:  types.StringValue("default"),
			Hosts:    existingHosts,
		}),
		BigQuery: types.ObjectNull(bigqueryAttrTypes),
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

	ch, d := state.clickhouse(ctx)
	if d.HasError() {
		t.Fatalf("unexpected diagnostics extracting clickhouse: %v", d)
	}
	if ch == nil {
		t.Fatal("clickhouse should be set")
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

	if !state.BigQuery.IsNull() {
		t.Fatal("bigquery should be null for clickhouse destination")
	}
}

func TestDestinationReadAPIIntoState_NullsHostsWhenMissing(t *testing.T) {
	ctx := context.Background()

	existingHosts, diags := types.ListValueFrom(ctx, types.StringType, []string{"old-host:8123"})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building existing hosts: %v", diags)
	}

	state := destinationModel{
		ClickHouse: mustClickhouseObject(t, ctx, &clickhouseModel{
			Hosts: existingHosts,
		}),
		BigQuery: types.ObjectNull(bigqueryAttrTypes),
	}

	result := map[string]interface{}{
		"name":            "Updated Destination",
		"destinationType": "clickhouse",
	}

	diags = (&destinationResource{}).readAPIIntoState(ctx, result, &state)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	ch, d := state.clickhouse(ctx)
	if d.HasError() {
		t.Fatalf("unexpected diagnostics extracting clickhouse: %v", d)
	}
	if ch == nil {
		t.Fatal("clickhouse should be set")
	}
	if !ch.Hosts.IsNull() {
		t.Fatalf("hosts should be null when API does not return hosts, got %v", ch.Hosts)
	}
}

func TestDestinationReadAPIIntoState_BigQuery(t *testing.T) {
	ctx := context.Background()

	state := destinationModel{
		ClickHouse: types.ObjectNull(clickhouseAttrTypes),
		BigQuery: mustBigqueryObject(t, ctx, &bigqueryModel{
			Credentials: types.StringValue("secret-json-key"),
		}),
	}

	result := map[string]interface{}{
		"name":            "BQ Destination",
		"destinationType": "bigquery",
		"project":   "my-project",
		"bqDataset": "my_dataset",
		"keyFile":   "__MASKED_BY_JITSU__",
	}

	diags := (&destinationResource{}).readAPIIntoState(ctx, result, &state)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	bq, d := state.bigquery(ctx)
	if d.HasError() {
		t.Fatalf("unexpected diagnostics extracting bigquery: %v", d)
	}
	if bq == nil {
		t.Fatal("bigquery should be set")
	}
	if bq.ProjectID.ValueString() != "my-project" {
		t.Fatalf("project_id mismatch: got %q", bq.ProjectID.ValueString())
	}
	if bq.BQDataset.ValueString() != "my_dataset" {
		t.Fatalf("bq_dataset mismatch: got %q", bq.BQDataset.ValueString())
	}
	// Credentials should be preserved from state, not overwritten with masked value.
	if bq.Credentials.ValueString() != "secret-json-key" {
		t.Fatalf("credentials should be preserved from state, got %q", bq.Credentials.ValueString())
	}

	if !state.ClickHouse.IsNull() {
		t.Fatal("clickhouse should be null for bigquery destination")
	}
}

func TestDestinationBuildPayload_ClickHouse(t *testing.T) {
	ctx := context.Background()

	hosts, diags := types.ListValueFrom(ctx, types.StringType, []string{"clickhouse:8123"})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building hosts: %v", diags)
	}

	plan := destinationModel{
		WorkspaceID:     types.StringValue("workspace-id"),
		ID:              types.StringValue("destination-id"),
		Name:            types.StringValue("ClickHouse"),
		DestinationType: types.StringValue("clickhouse"),
		ClickHouse: mustClickhouseObject(t, ctx, &clickhouseModel{
			Protocol: types.StringValue("http"),
			Hosts:    hosts,
			Username: types.StringValue("reporting"),
			Password: types.StringValue("secret"),
			Database: types.StringValue("default"),
			Cluster:  types.StringValue("default"),
		}),
		BigQuery: types.ObjectNull(bigqueryAttrTypes),
	}

	payload, err := (&destinationResource{}).buildPayload(ctx, &plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if payload["protocol"] != "http" {
		t.Fatalf("protocol mismatch: got %v", payload["protocol"])
	}
	if payload["username"] != "reporting" {
		t.Fatalf("username mismatch: got %v", payload["username"])
	}
	if payload["password"] != "secret" {
		t.Fatalf("password mismatch: got %v", payload["password"])
	}
	if payload["database"] != "default" {
		t.Fatalf("database mismatch: got %v", payload["database"])
	}
	if payload["cluster"] != "default" {
		t.Fatalf("cluster mismatch: got %v", payload["cluster"])
	}
	if _, ok := payload["credentials"]; ok {
		t.Fatal("credentials should not be set for clickhouse destination")
	}
}

func TestDestinationBuildPayload_BigQuery(t *testing.T) {
	ctx := context.Background()

	plan := destinationModel{
		WorkspaceID:     types.StringValue("workspace-id"),
		ID:              types.StringValue("destination-id"),
		Name:            types.StringValue("BigQuery"),
		DestinationType: types.StringValue("bigquery"),
		ClickHouse:      types.ObjectNull(clickhouseAttrTypes),
		BigQuery: mustBigqueryObject(t, ctx, &bigqueryModel{
			Credentials: types.StringValue("json-key"),
			ProjectID:   types.StringValue("my-project"),
			BQDataset:   types.StringValue("my_dataset"),
		}),
	}

	payload, err := (&destinationResource{}).buildPayload(ctx, &plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if payload["keyFile"] != "json-key" {
		t.Fatalf("keyFile mismatch: got %v", payload["keyFile"])
	}
	if payload["project"] != "my-project" {
		t.Fatalf("project mismatch: got %v", payload["project"])
	}
	if payload["bqDataset"] != "my_dataset" {
		t.Fatalf("bqDataset mismatch: got %v", payload["bqDataset"])
	}
	if _, ok := payload["hosts"]; ok {
		t.Fatal("hosts should not be set for bigquery destination")
	}
}
