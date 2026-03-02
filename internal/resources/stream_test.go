package resources

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestKeysToPayload_IncludesHint(t *testing.T) {
	ctx := context.Background()
	keys, diags := types.ListValueFrom(
		ctx,
		types.ObjectType{AttrTypes: streamKeyAttrTypes},
		[]streamKeyModel{
			{
				ID:        types.StringValue("js.browser-key"),
				Plaintext: types.StringValue("browser-secret-1234"),
			},
			{
				ID:        types.StringValue("s2s.server-key"),
				Plaintext: types.StringValue("abcd"),
			},
		},
	)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building keys: %v", diags)
	}

	got, err := keysToPayload(ctx, keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []map[string]string{
		{
			"id":        "js.browser-key",
			"plaintext": "browser-secret-1234",
			"hint":      "bro*234",
		},
		{
			"id":        "s2s.server-key",
			"plaintext": "abcd",
			"hint":      "abc*bcd",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keysToPayload mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestKeyHintFromPlaintext_MatchesConsoleBehavior(t *testing.T) {
	cases := map[string]string{
		"":        "*",
		"a":       "a*a",
		"ab":      "ab*ab",
		"abc":     "abc*abc",
		"abcd":    "abc*bcd",
		"1234567": "123*567",
	}

	for plaintext, want := range cases {
		got := keyHintFromPlaintext(plaintext)
		if got != want {
			t.Fatalf("keyHintFromPlaintext(%q) = %q, want %q", plaintext, got, want)
		}
	}
}

func TestKeysToPayload_NullList(t *testing.T) {
	ctx := context.Background()
	keys := types.ListNull(types.ObjectType{AttrTypes: streamKeyAttrTypes})

	got, err := keysToPayload(ctx, keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected empty payload for null keys, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty payload for null keys, got %#v", got)
	}
}

func TestKeysToPayload_UnknownList(t *testing.T) {
	ctx := context.Background()
	keys := types.ListUnknown(types.ObjectType{AttrTypes: streamKeyAttrTypes})

	got, err := keysToPayload(ctx, keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected empty payload for unknown keys, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty payload for unknown keys, got %#v", got)
	}
}

func TestKeysToPayload_EmptyList(t *testing.T) {
	ctx := context.Background()
	keys, diags := types.ListValueFrom(
		ctx,
		types.ObjectType{AttrTypes: streamKeyAttrTypes},
		[]streamKeyModel{},
	)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics building keys: %v", diags)
	}

	got, err := keysToPayload(ctx, keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected empty payload for empty keys, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty payload for empty keys, got %#v", got)
	}
}
