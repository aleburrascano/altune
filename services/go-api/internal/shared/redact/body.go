package redact

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

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

func decodedJSONDocument(body string) (any, bool) {
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, false
	}
	return document, decoder.Decode(&struct{}{}) == io.EOF
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

func encodedOrMasked(v any) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return Mask
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
