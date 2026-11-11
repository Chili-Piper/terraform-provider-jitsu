package resources

import (
	"fmt"
	"strings"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// splitImportID splits an import ID by "/" and returns the parts if count matches.
func splitImportID(id string, expectedParts int) []string {
	if expectedParts <= 0 {
		return nil
	}

	parts := strings.Split(id, "/")
	if len(parts) != expectedParts {
		return nil
	}
	for _, p := range parts {
		if p == "" {
			return nil
		}
	}
	return parts
}

// configureClient extracts the *client.Client from provider data.
// Returns nil if provider data is not yet available (during early validation).
func configureClient(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T", req.ProviderData),
		)
		return nil
	}
	return c
}

// Console stores object config as free-form JSON, so each field of a response is read on its own:
// a field that is absent or has an unexpected type becomes null instead of failing the whole read.

func stringOrNull(object map[string]interface{}, key string) types.String {
	if v, ok := object[key].(string); ok {
		return types.StringValue(v)
	}
	return types.StringNull()
}

func boolOrNull(object map[string]interface{}, key string) types.Bool {
	if v, ok := object[key].(bool); ok {
		return types.BoolValue(v)
	}
	return types.BoolNull()
}

func int64OrNull(object map[string]interface{}, key string) types.Int64 {
	switch v := object[key].(type) {
	case float64:
		return types.Int64Value(int64(v))
	case int64:
		return types.Int64Value(v)
	case int:
		return types.Int64Value(int64(v))
	default:
		return types.Int64Null()
	}
}
