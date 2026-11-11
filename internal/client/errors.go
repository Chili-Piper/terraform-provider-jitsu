package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

const (
	redacted       = "[redacted]"
	maxErrorDetail = 500
)

// Payload keys whose values are credentials.
var sensitivePayloadKeys = map[string]bool{
	"password":     true,
	"keyFile":      true,
	"plaintext":    true,
	"functionsEnv": true,
}

// responseError describes a failed Console call. It surfaces Console's own ApiError message and
// the schema-validation issues, never the echoed request, a stack, or the text of an unexpected
// exception. Issues are rebuilt from their structured fields because their message quotes the
// rejected value; the message is scrubbed of the credentials this request carried.
func (c *Client) responseError(method, endpoint string, status int, body []byte, payload interface{}) error {
	summary := fmt.Sprintf("%s %s returned %d (%s)", method, endpoint, status, http.StatusText(status))
	withoutDetail := fmt.Errorf("%s; see Console server logs for details", summary)
	detail := collapseSpace(consoleErrorDetail(body))
	if detail == "" {
		return withoutDetail
	}
	forms := secretForms(append(sensitiveValues(payload), tokenSecrets(c.authToken)...))
	detail = redact(detail, forms)
	// Redaction can itself spell out a secret, for one that contains the marker's text.
	if containsAny(detail, forms) {
		return withoutDetail
	}
	if runes := []rune(detail); len(runes) > maxErrorDetail {
		detail = string(runes[:maxErrorDetail]) + "…"
	}
	if containsAny(detail, forms) {
		return withoutDetail
	}
	return fmt.Errorf("%s: %s", summary, detail)
}

func containsAny(text string, forms []string) bool {
	for _, form := range forms {
		if strings.Contains(text, form) {
			return true
		}
	}
	return false
}

func consoleErrorDetail(body []byte) string {
	var response struct {
		Message  json.RawMessage `json:"message"`
		Error    json.RawMessage `json:"error"`
		ZodError json.RawMessage `json:"zodError"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return ""
	}
	var parts []string
	var message string
	if json.Unmarshal(response.Message, &message) == nil && message != "" {
		parts = append(parts, message)
	}
	// Console reports a rejected config object under "error" and a rejected request under "zodError".
	for _, raw := range []json.RawMessage{response.Error, response.ZodError} {
		var validation struct {
			Issues []zodIssue `json:"issues"`
		}
		if json.Unmarshal(raw, &validation) != nil {
			continue
		}
		for _, issue := range validation.Issues {
			parts = append(parts, issue.String())
		}
	}
	return strings.Join(parts, "; ")
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

// secretForms lists how each secret can read in a whitespace-collapsed message: as sent, and
// inside a JSON string as Go or JavaScript would escape it.
func secretForms(secrets []string) []string {
	seen := map[string]bool{}
	var forms []string
	add := func(form string) {
		if form = collapseSpace(form); form != "" && !seen[form] {
			seen[form] = true
			forms = append(forms, form)
		}
	}
	for _, secret := range secrets {
		add(secret)
		if quoted, err := json.Marshal(secret); err == nil {
			add(string(quoted[1 : len(quoted)-1]))
		}
		add(javascriptEscape(secret))
	}
	return forms
}

// javascriptEscape is the body of JSON.stringify(text): unlike Go it leaves <, >, &, U+2028 and
// U+2029 as they are.
func javascriptEscape(text string) string {
	var out strings.Builder
	for _, r := range text {
		switch r {
		case '"', '\\':
			out.WriteRune('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}

// redact replaces every occurrence of every form in one pass over the original text, so
// overlapping secrets are removed whole and a marker is never searched again.
func redact(text string, forms []string) string {
	type span struct{ start, end int }
	var spans []span
	for _, form := range forms {
		for from := 0; ; {
			i := strings.Index(text[from:], form)
			if i < 0 {
				break
			}
			spans = append(spans, span{from + i, from + i + len(form)})
			from += i + 1
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var out strings.Builder
	done := 0
	for _, s := range spans {
		if s.start >= done {
			out.WriteString(text[done:s.start])
			out.WriteString(redacted)
		}
		if s.end > done {
			done = s.end
		}
	}
	out.WriteString(text[done:])
	return out.String()
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
