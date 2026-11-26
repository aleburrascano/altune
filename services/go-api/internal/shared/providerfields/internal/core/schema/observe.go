package schema

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type Observed map[string]string

func Observe(raw []byte, into Observed) error {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("observe: %w", err)
	}
	walkObject("", root, into)
	return nil
}

func walkObject(prefix string, object map[string]any, into Observed) {
	for key, value := range object {
		walkValue(prefix+key, value, into)
	}
}

func walkValue(path string, value any, into Observed) {
	into.add(path, jsonType(value))
	switch v := value.(type) {
	case map[string]any:
		walkObject(path+".", v, into)
	case []any:
		walkElements(path+"[]", v, into)
	}
}

func walkElements(path string, elements []any, into Observed) {
	for _, element := range elements {
		walkElement(path, element, into)
	}
}

func walkElement(path string, element any, into Observed) {
	switch e := element.(type) {
	case map[string]any:
		walkObject(path+".", e, into)
	case []any:
		into.add(path, "array")
		walkElements(path+"[]", e, into)
	default:
		into.add(path, jsonType(e))
	}
}

func (o Observed) add(path, kind string) {
	kinds := strings.Split(o[path], "|")
	if o[path] == "" {
		kinds = nil
	}
	if !slices.Contains(kinds, kind) {
		kinds = append(kinds, kind)
		slices.Sort(kinds)
	}
	o[path] = strings.Join(kinds, "|")
}

func jsonType(value any) string {
	switch value.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	}
	return "null"
}

func (o Observed) Collapse(mapPaths []string) Observed {
	roots := slices.Clone(mapPaths)
	slices.SortStableFunc(roots, func(a, b string) int { return len(a) - len(b) })
	out := Observed{}
	for path, kinds := range o {
		collapsed := collapsePath(path, roots)
		for _, kind := range strings.Split(kinds, "|") {
			out.add(collapsed, kind)
		}
	}
	return out
}

func collapsePath(path string, roots []string) string {
	for _, root := range roots {
		rest, ok := strings.CutPrefix(path, root+".")
		if !ok {
			continue
		}
		end := strings.IndexAny(rest, ".[")
		if end < 0 {
			end = len(rest)
		}
		path = root + ".*" + rest[end:]
	}
	return path
}
