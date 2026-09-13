package resources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func validateUniqueStreamKeyIDs(keys types.List) error {
	seen := make(map[string]struct{}, len(keys.Elements()))
	for _, value := range keys.Elements() {
		key, ok := value.(types.Object)
		if !ok || key.IsNull() || key.IsUnknown() {
			continue
		}
		id, ok := key.Attributes()["id"].(types.String)
		if !ok || id.IsNull() || id.IsUnknown() {
			continue
		}
		if _, exists := seen[id.ValueString()]; exists {
			return fmt.Errorf("key IDs must be unique within each key list (duplicate ID %q)", id.ValueString())
		}
		seen[id.ValueString()] = struct{}{}
	}
	return nil
}

type uniqueStreamKeyIDs struct{}

func (uniqueStreamKeyIDs) Description(context.Context) string {
	return "Key IDs must be unique within each key list."
}

func (v uniqueStreamKeyIDs) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (uniqueStreamKeyIDs) ValidateList(_ context.Context, req validator.ListRequest, resp *validator.ListResponse) {
	if err := validateUniqueStreamKeyIDs(req.ConfigValue); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid stream key IDs", err.Error())
	}
}
