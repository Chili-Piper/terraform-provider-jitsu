package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const withoutDetailSuffix = "; see Console server logs for details"

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
// client fell back to the error without detail.
func errorDetail(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected HTTP failure")
	}
	if strings.HasSuffix(err.Error(), withoutDetailSuffix) {
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

func TestResponseErrorRedactsSentCredentials(t *testing.T) {
	keyFile := "{\n  \"private_key\": \"-----BEGIN KEY-----\\nkeyfile-canary\\n-----END KEY-----\"\n}"
	escapedKeyFile, err := json.Marshal(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		message string
		payload map[string]interface{}
		// want is the exact detail; "" means the client must fall back to the error without detail.
		want string
	}{
		"password": {
			message: "rejected password-canary",
			payload: map[string]interface{}{"password": "password-canary"},
			want:    "rejected [redacted]",
		},
		"stream key": {
			message: "rejected key-canary",
			payload: map[string]interface{}{"publicKeys": []map[string]string{{"id": "k", "plaintext": "key-canary"}}},
			want:    "rejected [redacted]",
		},
		"function environment": {
			message: "rejected env-canary",
			payload: map[string]interface{}{"data": map[string]interface{}{"functionsEnv": map[string]interface{}{"API_KEY": "env-canary"}}},
			want:    "rejected [redacted]",
		},
		"key file": {
			message: "rejected " + keyFile,
			payload: map[string]interface{}{"keyFile": keyFile},
			want:    "rejected [redacted]",
		},
		"go-escaped key file": {
			message: "rejected " + string(escapedKeyFile[1:len(escapedKeyFile)-1]),
			payload: map[string]interface{}{"keyFile": keyFile},
			want:    "rejected [redacted]",
		},
		"auth token": {
			message: "rejected key-id:token-secret-canary and token-secret-canary",
			want:    "rejected [redacted] and [redacted]",
		},
		"whitespace variant": {
			message: "received 'alpha\nbeta'",
			payload: map[string]interface{}{"password": "alpha beta"},
			want:    "received '[redacted]'",
		},
		"javascript-escaped html characters": {
			message: `received "A<\nB"`,
			payload: map[string]interface{}{"password": "A<\nB"},
			want:    `received "[redacted]"`,
		},
		"javascript-escaped line separator": {
			message: "received \"alpha \\nbravo\"",
			payload: map[string]interface{}{"password": "alpha \nbravo"},
			want:    `received "[redacted]"`,
		},
		"javascript-escaped paragraph separator": {
			message: "received \"alpha\u2029\\nbravo\"",
			payload: map[string]interface{}{"password": "alpha\u2029\nbravo"},
			want:    `received "[redacted]"`,
		},
		"overlapping secrets": {
			message: "received abcde",
			payload: map[string]interface{}{"publicKeys": []map[string]string{{"plaintext": "abcd"}, {"plaintext": "bcde"}}},
			want:    "received [redacted]",
		},
		"repeated secret": {
			message: "received canary-one and canary-one",
			payload: map[string]interface{}{"publicKeys": []map[string]string{{"plaintext": "canary-one"}, {"plaintext": "canary-one"}}},
			want:    "received [redacted] and [redacted]",
		},
		"secret inside the marker": {
			message: "rejected red",
			payload: map[string]interface{}{"password": "red"},
			want:    "",
		},
		"redaction spelling out another secret": {
			message: "received abcxdef",
			payload: map[string]interface{}{"publicKeys": []map[string]string{{"plaintext": "abc" + redacted + "def"}, {"plaintext": "x"}}},
			want:    "",
		},
		"spelled-out secret cut by truncation": {
			message: strings.Repeat("q", maxErrorDetail-15) + "abcxdef",
			payload: map[string]interface{}{"publicKeys": []map[string]string{{"plaintext": "abc" + redacted + "def"}, {"plaintext": "x"}}},
			want:    "",
		},
		"secret completed by the truncation mark": {
			message: strings.Repeat("q", maxErrorDetail-3) + "abcdef",
			payload: map[string]interface{}{"password": "abc…"},
			want:    "",
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := failingConsole(t, http.StatusBadRequest, messageBody(t, tc.message))
			_, err := c.Update(context.Background(), "ws", "destination", "d", tc.payload)
			if got := errorDetail(t, err); got != tc.want {
				t.Errorf("detail = %q, want %q", got, tc.want)
			}
			if strings.Contains(err.Error(), "canary") {
				t.Errorf("credential leaked into error: %v", err)
			}
		})
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
