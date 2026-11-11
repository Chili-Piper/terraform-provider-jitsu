package resources

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chilipiper/terraform-provider-jitsu/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
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
	if got != nil {
		t.Fatalf("expected no payload for null keys, got %#v", got)
	}
}

func TestKeysToPayload_UnknownList(t *testing.T) {
	ctx := context.Background()
	keys := types.ListUnknown(types.ObjectType{AttrTypes: streamKeyAttrTypes})

	if _, err := keysToPayload(ctx, keys); err == nil {
		t.Fatal("expected an error for unknown keys")
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

func streamDriftTestState(t *testing.T, r resource.Resource, model interface{}) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	var schema resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schema)
	state := tfsdk.State{Schema: schema.Schema}
	if d := state.Set(ctx, model); d.HasError() {
		t.Fatal(d)
	}
	return state
}

func streamDriftTestKeys(t *testing.T, ids ...string) types.List {
	t.Helper()
	keys := make([]streamKeyModel, len(ids))
	for i, id := range ids {
		keys[i] = streamKeyModel{ID: types.StringValue(id), Plaintext: types.StringValue("secret-" + id)}
	}
	list, d := types.ListValueFrom(context.Background(), types.ObjectType{AttrTypes: streamKeyAttrTypes}, keys)
	if d.HasError() {
		t.Fatal(d)
	}
	return list
}

func TestAdversarialStreamKeyDrift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remote []map[string]string
		want   int
	}{
		{"revoked", []map[string]string{}, 0},
		{"unauthorized addition", []map[string]string{{"id": "managed"}, {"id": "external"}}, 2},
		{"unchanged", []map[string]string{{"id": "managed"}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				json.NewEncoder(w).Encode(map[string]interface{}{"id": "stream", "name": "Stream", "publicKeys": tc.remote, "privateKeys": tc.remote})
			}))
			defer server.Close()
			r := &streamResource{client: client.New(server.URL, "test", "", "test")}
			model := streamModel{WorkspaceID: types.StringValue("ws"), ID: types.StringValue("stream"), Name: types.StringValue("Stream"), PublicKeys: streamDriftTestKeys(t, "managed"), PrivateKeys: streamDriftTestKeys(t, "managed")}
			state := streamDriftTestState(t, r, &model)
			resp := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			var got streamModel
			if d := resp.State.Get(context.Background(), &got); d.HasError() {
				t.Fatal(d)
			}
			for _, list := range []types.List{got.PublicKeys, got.PrivateKeys} {
				if len(list.Elements()) != tc.want {
					t.Errorf("refresh reports %d keys; remote has %d", len(list.Elements()), tc.want)
				}
				var keys []streamKeyModel
				if d := list.ElementsAs(context.Background(), &keys, false); d.HasError() {
					t.Fatal(d)
				}
				for _, key := range keys {
					if key.ID.ValueString() == "managed" && key.Plaintext.ValueString() != "secret-managed" {
						t.Error("lost managed plaintext")
					}
					if key.ID.ValueString() == "external" && !key.Plaintext.IsNull() {
						t.Error("invented external plaintext")
					}
				}
			}
		})
	}
}

func TestAccStreamKeyDriftConsole(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	ctx := context.Background()
	c := client.New(os.Getenv("JITSU_CONSOLE_URL"), os.Getenv("JITSU_AUTH_TOKEN"), os.Getenv("JITSU_DATABASE_URL"), "adversarial-test")
	defer c.Close()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ws, err := c.WorkspaceCreate(ctx, "Adversarial "+suffix, "adversarial-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	defer c.WorkspaceDelete(ctx, ws)
	streamID := "audit_stream_" + suffix
	if _, err := c.Create(ctx, ws, "stream", map[string]interface{}{"id": streamID, "name": "Audit"}); err != nil {
		t.Fatal(err)
	}
	defer c.Delete(ctx, ws, "stream", streamID)
	t.Run("key drift", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			keys []map[string]string
		}{
			{"revoked", []map[string]string{}},
			{"added", []map[string]string{{"id": "managed", "plaintext": "secret-managed"}, {"id": "external", "plaintext": "secret-external"}}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := c.Update(ctx, ws, "stream", streamID, map[string]interface{}{"publicKeys": tc.keys, "privateKeys": tc.keys}); err != nil {
					t.Fatal(err)
				}
				r := &streamResource{client: c}
				model := streamModel{WorkspaceID: types.StringValue(ws), ID: types.StringValue(streamID), Name: types.StringValue("Audit"), PublicKeys: streamDriftTestKeys(t, "managed"), PrivateKeys: streamDriftTestKeys(t, "managed")}
				state := streamDriftTestState(t, r, &model)
				resp := resource.ReadResponse{State: state}
				r.Read(ctx, resource.ReadRequest{State: state}, &resp)
				if resp.Diagnostics.HasError() {
					t.Fatal(resp.Diagnostics)
				}
				if d := resp.State.Get(ctx, &model); d.HasError() {
					t.Fatal(d)
				}
				if len(model.PublicKeys.Elements()) != len(tc.keys) || len(model.PrivateKeys.Elements()) != len(tc.keys) {
					t.Fatalf("Console has %d keys per list; refreshed state has %d public, %d private", len(tc.keys), len(model.PublicKeys.Elements()), len(model.PrivateKeys.Elements()))
				}
			})
		}
	})
}

func TestKeysToPayloadRejectsDuplicateIDs(t *testing.T) {
	ctx := context.Background()
	keys, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: streamKeyAttrTypes}, []streamKeyModel{
		{ID: types.StringValue("duplicate"), Plaintext: types.StringValue("first-secret")},
		{ID: types.StringValue("duplicate"), Plaintext: types.StringValue("second-secret")},
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	payload, err := keysToPayload(ctx, keys)
	if err == nil {
		remote := []interface{}{map[string]interface{}{"id": "duplicate"}, map[string]interface{}{"id": "duplicate"}}
		refreshed, diags := refreshStreamKeys(ctx, keys, remote)
		if diags.HasError() {
			t.Fatal(diags)
		}
		t.Fatalf("duplicate IDs reached the API (%d keys), then refresh changed plaintext state: before=%s after=%s", len(payload), keys, refreshed)
	}
	if !strings.Contains(err.Error(), "key IDs must be unique") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestStreamKeyIDUniquenessValidation(t *testing.T) {
	keyType := types.ObjectType{AttrTypes: streamKeyAttrTypes}
	keys := func(ids ...types.String) types.List {
		values := make([]attr.Value, len(ids))
		for i, id := range ids {
			values[i] = types.ObjectValueMust(streamKeyAttrTypes, map[string]attr.Value{
				"id": id, "plaintext": types.StringUnknown(),
			})
		}
		return types.ListValueMust(keyType, values)
	}
	for _, tc := range []struct {
		name      string
		keys      types.List
		wantError bool
	}{
		{"omitted", types.ListNull(keyType), false},
		{"unknown list", types.ListUnknown(keyType), false},
		{"unique IDs", keys(types.StringValue("first"), types.StringValue("second")), false},
		{"unknown IDs", keys(types.StringUnknown(), types.StringUnknown()), false},
		{"unknown object", types.ListValueMust(keyType, []attr.Value{types.ObjectUnknown(streamKeyAttrTypes)}), false},
		{"duplicate IDs", keys(types.StringValue("duplicate"), types.StringValue("duplicate")), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resp validator.ListResponse
			uniqueStreamKeyIDs{}.ValidateList(context.Background(), validator.ListRequest{ConfigValue: tc.keys}, &resp)
			if resp.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
			}
		})
	}
}

func TestKeysToPayloadRejectsEmptyID(t *testing.T) {
	ctx := context.Background()
	keys, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: streamKeyAttrTypes}, []streamKeyModel{
		{ID: types.StringValue(""), Plaintext: types.StringValue("first-secret")},
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	if _, err := keysToPayload(ctx, keys); err == nil || !strings.Contains(err.Error(), "key IDs must not be empty") {
		t.Fatalf("expected empty ID to be rejected before mutation, got %v", err)
	}
}

func TestStreamKeyIDValidationDefersUnknown(t *testing.T) {
	ctx := context.Background()
	var resp validator.StringResponse
	nonEmptyStreamKeyID{}.ValidateString(ctx, validator.StringRequest{ConfigValue: types.StringUnknown()}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatal(resp.Diagnostics)
	}
	keys, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: streamKeyAttrTypes}, []streamKeyModel{
		{ID: types.StringUnknown(), Plaintext: types.StringValue("first-secret")},
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	if _, err := keysToPayload(ctx, keys); err == nil || !strings.Contains(err.Error(), "key IDs must be known before applying") {
		t.Fatalf("expected unresolved ID to be rejected at apply, got %v", err)
	}
}

func TestStreamReadExistingEmptyKeyID(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remote    map[string]interface{}
		wantError bool
	}{
		{"existing empty ID", map[string]interface{}{"id": ""}, false},
		{"missing ID", map[string]interface{}{}, true},
		{"non-string ID", map[string]interface{}{"id": 42}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				json.NewEncoder(w).Encode(map[string]interface{}{"id": "stream", "name": "Stream", "publicKeys": []interface{}{tc.remote}})
			}))
			defer server.Close()
			r := &streamResource{client: client.New(server.URL, "test", "", "test")}
			model := streamModel{
				WorkspaceID: types.StringValue("ws"), ID: types.StringValue("stream"), Name: types.StringValue("Stream"),
				PublicKeys: streamDriftTestKeys(t, ""), PrivateKeys: types.ListNull(types.ObjectType{AttrTypes: streamKeyAttrTypes}),
			}
			state := streamDriftTestState(t, r, &model)
			resp := resource.ReadResponse{State: state}
			r.Read(ctx, resource.ReadRequest{State: state}, &resp)
			if resp.Diagnostics.HasError() != tc.wantError {
				t.Fatalf("unexpected refresh diagnostics for existing empty-ID state: %v", resp.Diagnostics)
			}
			if tc.wantError {
				return
			}
			var got streamModel
			if diags := resp.State.Get(ctx, &got); diags.HasError() {
				t.Fatal(diags)
			}
			if !got.PublicKeys.Equal(model.PublicKeys) {
				t.Fatalf("refresh changed existing key state: %s", got.PublicKeys)
			}
		})
	}
}

func TestAccStream_emptyKeyDoesNotSilentlyRetainCredential(t *testing.T) {
	c, ws := auditConsole(t)
	ctx := context.Background()
	db, err := sql.Open("postgres", os.Getenv("JITSU_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, field := range []string{"publicKeys", "privateKeys"} {
		t.Run(field, func(t *testing.T) {
			id := "empty_" + field + "_" + ws
			_, err := c.Create(ctx, ws, "stream", map[string]interface{}{"id": id, "name": "Empty key test"})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Delete(ctx, ws, "stream", id)
			_, err = c.Update(ctx, ws, "stream", id, map[string]interface{}{field: []map[string]string{{"id": "managed", "plaintext": "previous-working-secret", "hint": "pre*ret"}}})
			if err != nil {
				t.Fatal(err)
			}
			readHash := func() string {
				var hash string
				if err := db.QueryRowContext(ctx, `SELECT config->$1->0->>'hash' FROM newjitsu."ConfigurationObject" WHERE id=$2 AND "workspaceId"=$3`, field, id, ws).Scan(&hash); err != nil {
					t.Fatal(err)
				}
				return hash
			}
			before := readHash()
			r := &streamResource{client: c}
			keyType := types.ObjectType{AttrTypes: streamKeyAttrTypes}
			model := streamModel{WorkspaceID: types.StringValue(ws), ID: types.StringValue(id), Name: types.StringValue("Empty key test"), PublicKeys: types.ListNull(keyType), PrivateKeys: types.ListNull(keyType)}
			keys := func(secret string) types.List {
				value, d := types.ListValueFrom(ctx, keyType, []streamKeyModel{{ID: types.StringValue("managed"), Plaintext: types.StringValue(secret)}})
				if d.HasError() {
					t.Fatal(d)
				}
				return value
			}
			if field == "publicKeys" {
				model.PublicKeys = keys("previous-working-secret")
			} else {
				model.PrivateKeys = keys("previous-working-secret")
			}
			state := createRollbackTestState(t, r, &model)
			if field == "publicKeys" {
				model.PublicKeys = keys("")
			} else {
				model.PrivateKeys = keys("")
			}
			plan := createRollbackTestState(t, r, &model)
			resp := resource.UpdateResponse{State: state}
			r.Update(ctx, resource.UpdateRequest{State: state, Plan: tfsdk.Plan{Raw: plan.Raw, Schema: plan.Schema}}, &resp)
			unchanged := readHash() == before
			if !resp.Diagnostics.HasError() {
				t.Fatalf("empty %s rotation reported success; previous credential hash unchanged=%t", field, unchanged)
			}
			if !unchanged {
				t.Fatal("rejected update changed the credential")
			}
			t.Log("Empty rotation rejected and previous credential left unchanged")
		})
	}
}
