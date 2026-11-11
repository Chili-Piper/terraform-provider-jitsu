package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

const maxErrorDetail = 500

// Payload keys whose values are credentials.
var sensitivePayloadKeys = map[string]bool{
	"password":     true,
	"keyFile":      true,
	"plaintext":    true,
	"functionsEnv": true,
}

// responseError describes a failed Console call. It surfaces the schema-validation issues, rebuilt
// from their structured fields because their own text quotes the rejected value, and Console's
// ApiError message unless that message contains a credential this request carried. The echoed
// request, stacks and the text of unexpected exceptions are never surfaced.
func (c *Client) responseError(method, endpoint string, status int, body []byte, payload interface{}) error {
	summary := fmt.Sprintf("%s %s returned %d (%s)", method, endpoint, status, http.StatusText(status))
	var response struct {
		Message  json.RawMessage `json:"message"`
		Error    json.RawMessage `json:"error"`
		ZodError json.RawMessage `json:"zodError"`
	}
	_ = json.Unmarshal(body, &response)

	var parts []string
	var message string
	if json.Unmarshal(response.Message, &message) == nil {
		message = collapseSpace(message)
		if message != "" && !containsCredential(message, append(sensitiveValues(payload), tokenSecrets(c.authToken)...)) {
			parts = append(parts, message)
		}
	}
	// Console reports a rejected config object under "error" and a rejected request under "zodError".
	for _, raw := range []json.RawMessage{response.Error, response.ZodError} {
		var validation struct {
			Issues []zodIssue `json:"issues"`
		}
		if json.Unmarshal(raw, &validation) == nil {
			for _, issue := range validation.Issues {
				parts = append(parts, issue.String())
			}
		}
	}
	if len(parts) == 0 {
		return fmt.Errorf("%s; see Console server logs for details", summary)
	}
	detail := strings.Join(parts, "; ")
	if runes := []rune(detail); len(runes) > maxErrorDetail {
		detail = string(runes[:maxErrorDetail]) + "…"
	}
	return fmt.Errorf("%s: %s", summary, detail)
}

type zodIssue struct {
	Path     []interface{}   `json:"path"`
	Code     string          `json:"code"`
	Expected json.RawMessage `json:"expected"`
	Received json.RawMessage `json:"received"`
	Options  []interface{}   `json:"options"`
}

var (
	zodIssueCode = regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`)
	zodTypeName  = regexp.MustCompile(`^[a-z]+$`)
)

// String names the field and what the schema wants from it. The path can hold a key the request
// supplied; everything else comes from the schema. The received value is read solely to tell a
// missing field from a mistyped one.
func (i zodIssue) String() string {
	field := "value"
	if len(i.Path) > 0 {
		segments := make([]string, len(i.Path))
		for n, segment := range i.Path {
			segments[n] = fmt.Sprint(segment)
		}
		field = strings.Join(segments, ".")
	}
	return field + ": " + i.problem()
}

func (i zodIssue) problem() string {
	switch i.Code {
	case "invalid_type":
		if string(i.Received) == `"undefined"` {
			return "required"
		}
		var expected string
		if json.Unmarshal(i.Expected, &expected) == nil && zodTypeName.MatchString(expected) {
			return "expected " + expected
		}
	case "invalid_enum_value":
		options := make([]string, 0, len(i.Options))
		for _, option := range i.Options {
			switch option.(type) {
			case string, float64:
				options = append(options, fmt.Sprint(option))
			}
		}
		if len(options) > 0 {
			return "must be one of " + strings.Join(options, ", ")
		}
	}
	if zodIssueCode.MatchString(i.Code) {
		return strings.ReplaceAll(i.Code, "_", " ")
	}
	return "invalid"
}

func collapseSpace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// containsCredential reports whether a whitespace-collapsed message holds any of the secrets, as
// sent or as JSON.stringify would escape it.
func containsCredential(message string, secrets []string) bool {
	for _, secret := range secrets {
		var escaped bytes.Buffer
		encoder := json.NewEncoder(&escaped)
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(secret)
		quoted := strings.TrimSuffix(escaped.String(), "\n")
		for _, form := range []string{secret, quoted[1 : len(quoted)-1]} {
			if form = collapseSpace(form); form != "" && strings.Contains(message, form) {
				return true
			}
		}
	}
	return false
}

func tokenSecrets(authToken string) []string {
	secrets := []string{authToken}
	if _, secret, found := strings.Cut(authToken, ":"); found {
		secrets = append(secrets, secret)
	}
	return secrets
}

func sensitiveValues(payload interface{}) []string {
	if payload == nil {
		return nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var decoded interface{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil
	}
	var values []string
	var walk func(value interface{}, sensitive bool)
	walk = func(value interface{}, sensitive bool) {
		switch typed := value.(type) {
		case map[string]interface{}:
			for key, child := range typed {
				walk(child, sensitive || sensitivePayloadKeys[key])
			}
		case []interface{}:
			for _, child := range typed {
				walk(child, sensitive)
			}
		case string:
			if sensitive {
				values = append(values, typed)
			}
		}
	}
	walk(decoded, false)
	return values
}
