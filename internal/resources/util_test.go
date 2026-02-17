package resources

import (
	"reflect"
	"testing"
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
