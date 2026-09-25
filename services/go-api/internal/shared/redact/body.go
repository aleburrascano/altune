package redact

import (
	"bytes"
	"encoding/json"
	"strings"
)

// SecretsInBody masks the credential-bearing fields of an HTTP request or
// response body before it is stored: a JSON document is masked by field name at
// any depth, and anything else (form-encoded, HTML, a truncated fragment) falls
// back to the text masking Secrets does, so a malformed body degrades instead
// of failing. It is idempotent, which httptrace replay match keys depend on.
func SecretsInBody(body string) string {
	scrubbed, isJSON := scrubbedJSON(body)
	if !isJSON {
		return Secrets(body)
	}
	return scrubbed
}

func scrubbedJSON(body string) (string, bool) {
	document, isJSON := decodedJSONDocument(body)
	if !isJSON {
		return "", false
	}
	scrubbed, masked := withoutCredentials(document)
	if !masked {
		return body, true
	}
	return encodedOrMasked(scrubbed), true
}

// decodedJSONDocument reports false for anything but exactly one JSON value, so
// trailing bytes cannot smuggle a credential past the walk below. Numbers are
// kept as json.Number so re-encoding cannot change a value the body carried.
func decodedJSONDocument(body string) (any, bool) {
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, false
	}
	return document, !decoder.More()
}

func withoutCredentials(v any) (any, bool) {
	switch value := v.(type) {
	case map[string]any:
		return maskedFields(value)
	case []any:
		return maskedItems(value)
	case string:
		masked := Secrets(value)
		return masked, masked != value
	}
	return v, false
}

func maskedFields(fields map[string]any) (map[string]any, bool) {
	masked := false
	for name, v := range fields {
		if IsSecretKey(name) {
			fields[name] = Mask
			masked = true
			continue
		}
		scrubbed, fieldMasked := withoutCredentials(v)
		fields[name] = scrubbed
		masked = masked || fieldMasked
	}
	return fields, masked
}

func maskedItems(items []any) ([]any, bool) {
	masked := false
	for i, item := range items {
		scrubbed, itemMasked := withoutCredentials(item)
		items[i] = scrubbed
		masked = masked || itemMasked
	}
	return items, masked
}

// encodedOrMasked drops the whole document when a scrubbed value will not
// re-encode: falling back to text masking would keep the credential just found.
func encodedOrMasked(v any) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return Mask
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
