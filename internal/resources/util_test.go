package resources

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestSplitImportID_Valid(t *testing.T) {
	got := splitImportID("workspace/object", 2)
	want := []string{"workspace", "object"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitImportID returned %v, want %v", got, want)
	}
}

func TestSplitImportID_RejectsExtraSegments(t *testing.T) {
	if got := splitImportID("workspace/object/extra", 2); got != nil {
		t.Fatalf("splitImportID returned %v, want nil", got)
	}
}

func TestSplitImportID_RejectsEmptySegments(t *testing.T) {
	if got := splitImportID("workspace/", 2); got != nil {
		t.Fatalf("splitImportID returned %v, want nil", got)
	}
}

func TestResponseFieldHelpers(t *testing.T) {
	object := map[string]interface{}{
		"text":     "value",
		"empty":    "",
		"flag":     false,
		"float":    float64(5432),
		"fraction": 1.9,
		"int":      7,
		"int64":    int64(8),
		"null":     nil,
	}

	for key, want := range map[string]types.String{
		"text":    types.StringValue("value"),
		"empty":   types.StringValue(""),
		"flag":    types.StringNull(),
		"float":   types.StringNull(),
		"null":    types.StringNull(),
		"missing": types.StringNull(),
	} {
		if got := stringOrNull(object, key); !got.Equal(want) {
			t.Errorf("stringOrNull(%q) = %v, want %v", key, got, want)
		}
	}

	for key, want := range map[string]types.Bool{
		"flag":    types.BoolValue(false),
		"text":    types.BoolNull(),
		"null":    types.BoolNull(),
		"missing": types.BoolNull(),
	} {
		if got := boolOrNull(object, key); !got.Equal(want) {
			t.Errorf("boolOrNull(%q) = %v, want %v", key, got, want)
		}
	}

	for key, want := range map[string]types.Int64{
		"float":    types.Int64Value(5432),
		"fraction": types.Int64Value(1),
		"int":      types.Int64Value(7),
		"int64":    types.Int64Value(8),
		"text":     types.Int64Null(),
		"null":     types.Int64Null(),
		"missing":  types.Int64Null(),
	} {
		if got := int64OrNull(object, key); !got.Equal(want) {
			t.Errorf("int64OrNull(%q) = %v, want %v", key, got, want)
		}
	}

	if got := stringOrNull(nil, "text"); !got.IsNull() {
		t.Errorf("stringOrNull on a nil object = %v, want null", got)
	}
}
