package resources

import (
	"fmt"
	"strings"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
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
