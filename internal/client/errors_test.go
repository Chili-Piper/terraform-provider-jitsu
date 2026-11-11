package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func failingConsole(t *testing.T, status int, body string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return New(server.URL, "key-id:token-secret-canary", "", "test")
}

// errorDetail is the part of a client error that came from the Console response, or "" when the
// client reported the failure without detail.
func errorDetail(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected HTTP failure")
	}
	if strings.HasSuffix(err.Error(), "; see Console server logs for details") {
		return ""
	}
	_, detail, found := strings.Cut(err.Error(), "): ")
	if !found {
		t.Fatalf("unexpected error format: %v", err)
	}
	return detail
}

func messageBody(t *testing.T, message string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"message": message})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestResponseErrorSurfacesConsoleMessage(t *testing.T) {
	c := failingConsole(t, http.StatusBadRequest, `{"message":"Link from 'site' to 'warehouse' already exists"}`)
	_, err := c.Create(context.Background(), "ws", "link", map[string]interface{}{"fromId": "site", "toId": "warehouse"})
	if err == nil || !strings.Contains(err.Error(), "400 (Bad Request): Link from 'site' to 'warehouse' already exists") {
		t.Fatalf("Console message missing from error: %v", err)
	}
}

func TestResponseErrorDescribesValidationIssues(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"rejected config object": {
			body: `{"message":"Failed to validate schema of destination",` +
				`"object":{"protocol":"tcp","password":"password-canary"},` +
				`"error":{"name":"ZodError","issues":[{"code":"invalid_enum_value","received":"tcp","options":["http","https"],` +
				`"path":["protocol"],"message":"Invalid enum value. Expected 'http' | 'https', received 'tcp'"}]},` +
				`"stack":"Error: at password-canary"}`,
			want: "Failed to validate schema of destination; protocol: must be one of http, https",
		},
		"rejected request": {
			body: `{"message":"Can't parse request body","body":{"password":"password-canary"},` +
				`"zodError":{"issues":[{"code":"invalid_type","expected":"string","received":"undefined","path":["toId"],"message":"Required"}]}}`,
			want: "Can't parse request body; toId: required",
		},
		"both containers": {
			body: `{"error":{"issues":[{"code":"invalid_type","expected":"number","received":"string","path":["port"]}]},` +
				`"zodError":{"issues":[{"code":"too_small","path":["hosts",0]}]}}`,
			want: "port: expected number; hosts.0: too small",
		},
		"received value is never quoted": {
			body: `{"error":{"issues":[` +
				`{"code":"invalid_enum_value","received":"password-canary","options":["a"],"path":["protocol"],"message":"received 'password-canary'"},` +
				`{"code":"invalid_type","expected":"password-canary","received":"password-canary","path":["host"],"message":"password-canary"},` +
				`{"code":"password-canary","path":[],"message":"password-canary"},` +
				`{"code":"custom","path":["name"],"message":"password-canary","params":{"value":"password-canary"}}]}}`,
			want: "protocol: must be one of a; host: invalid type; value: invalid; name: custom",
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := failingConsole(t, http.StatusInternalServerError, tc.body)
			_, err := c.Create(context.Background(), "ws", "destination", map[string]interface{}{"id": "d"})
			if got := errorDetail(t, err); got != tc.want {
				t.Errorf("detail = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResponseErrorLeavesOutMessageHoldingCredential(t *testing.T) {
	keyFile := "{\n  \"private_key\": \"-----BEGIN KEY-----\\nkeyfile-canary\\n-----END KEY-----\"\n}"
	escapedKeyFile, err := json.Marshal(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		message string
		payload map[string]interface{}
	}{
		"password": {
			message: "rejected password-canary",
			payload: map[string]interface{}{"password": "password-canary"},
		},
		"stream key": {
			message: "rejected key-canary",
			payload: map[string]interface{}{"publicKeys": []map[string]string{{"id": "k", "plaintext": "key-canary"}}},
		},
		"function environment": {
			message: "rejected env-canary",
			payload: map[string]interface{}{"data": map[string]interface{}{"functionsEnv": map[string]interface{}{"API_KEY": "env-canary"}}},
		},
		"key file": {
			message: "rejected " + keyFile,
			payload: map[string]interface{}{"keyFile": keyFile},
		},
		"json-escaped key file": {
			message: "rejected " + string(escapedKeyFile[1:len(escapedKeyFile)-1]),
			payload: map[string]interface{}{"keyFile": keyFile},
		},
		"json-escaped html characters": {
			message: `rejected "canary<\n&"`,
			payload: map[string]interface{}{"password": "canary<\n&"},
		},
		"whitespace variant": {
			message: "rejected canary\none",
			payload: map[string]interface{}{"password": "canary one"},
		},
		"auth token": {
			message: "rejected key-id:token-secret-canary",
		},
		"auth token secret": {
			message: "rejected token-secret-canary",
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := failingConsole(t, http.StatusBadRequest, messageBody(t, tc.message))
			_, err := c.Update(context.Background(), "ws", "destination", "d", tc.payload)
			if got := errorDetail(t, err); got != "" {
				t.Errorf("message holding a credential surfaced: %q", got)
			}
		})
	}
}

func TestResponseErrorKeepsIssuesWhenMessageIsLeftOut(t *testing.T) {
	body := `{"message":"rejected password-canary",` +
		`"error":{"issues":[{"code":"invalid_type","expected":"string","received":"undefined","path":["host"]}]}}`
	c := failingConsole(t, http.StatusInternalServerError, body)
	_, err := c.Create(context.Background(), "ws", "destination", map[string]interface{}{"password": "password-canary"})
	if got, want := errorDetail(t, err), "host: required"; got != want {
		t.Errorf("detail = %q, want %q", got, want)
	}
}

func TestResponseErrorDropsUnexpectedExceptionText(t *testing.T) {
	for name, body := range map[string]string{
		"exception":  `{"error":"Invalid prisma invocation with stored-canary","details":"stack stored-canary","stackArray":["stored-canary"]}`,
		"plain text": "upstream said stored-canary",
		"non-string": `{"message":{"nested":"stored-canary"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			c := failingConsole(t, http.StatusInternalServerError, body)
			_, err := c.Read(context.Background(), "ws", "destination", "d")
			if got := errorDetail(t, err); got != "" {
				t.Errorf("unexpected response text surfaced: %q", got)
			}
		})
	}
}

func TestResponseErrorTruncatesLongMessages(t *testing.T) {
	c := failingConsole(t, http.StatusBadRequest, messageBody(t, strings.Repeat("é", 5000)))
	_, err := c.Read(context.Background(), "ws", "destination", "d")
	if got, want := errorDetail(t, err), strings.Repeat("é", maxErrorDetail)+"…"; got != want {
		t.Fatalf("detail has %d runes, want %d", len([]rune(got)), len([]rune(want)))
	}
}
