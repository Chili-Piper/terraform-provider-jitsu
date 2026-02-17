package resources

import (
	"context"
	"reflect"
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
		Protocol: types.StringValue("http"),
		Username: types.StringValue("reporting"),
		Database: types.StringValue("default"),
		Hosts:    existingHosts,
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

	if !state.Protocol.IsNull() {
		t.Fatalf("protocol should be null, got %v", state.Protocol)
	}
	if !state.Username.IsNull() {
		t.Fatalf("username should be null, got %v", state.Username)
	}
	if !state.Database.IsNull() {
		t.Fatalf("database should be null, got %v", state.Database)
	}

	var hosts []string
	diags = state.Hosts.ElementsAs(ctx, &hosts, false)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics reading hosts: %v", diags)
	}
	if !reflect.DeepEqual(hosts, []string{"new-host:8123"}) {
		t.Fatalf("hosts mismatch: got %v", hosts)
	}
}

func TestDestinationReadAPIIntoState_NullsHostsWhenMissing(t *testing.T) {
	ctx := context.Background()

	existingHosts, diags := types.ListValueFrom(ctx, types.StringType, []string{"old-host:8123"})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building existing hosts: %v", diags)
	}

	state := destinationModel{
		Hosts: existingHosts,
	}

	result := map[string]interface{}{
		"name":            "Updated Destination",
		"destinationType": "clickhouse",
	}

	diags = (&destinationResource{}).readAPIIntoState(ctx, result, &state)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if !state.Hosts.IsNull() {
		t.Fatalf("hosts should be null when API does not return hosts, got %v", state.Hosts)
	}
}
