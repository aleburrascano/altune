package goapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

var timeType = reflect.TypeOf(time.Time{})

func loadContractFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "contract", name))
	if err != nil {
		t.Fatalf("read contract fixture %s: %v", name, err)
	}
	return data
}

func jsonTagName(f reflect.StructField) (name string, omitEmpty, ok bool) {
	if !f.IsExported() {
		return "", false, false
	}
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false, false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = f.Name
	}
	for _, opt := range parts[1:] {
		if opt == "omitempty" {
			omitEmpty = true
		}
	}
	return name, omitEmpty, true
}

func uncoveredKeys(fixture json.RawMessage, typ reflect.Type) []string {
	return walkUncovered("", fixture, typ)
}

func walkUncovered(path string, fixture json.RawMessage, typ reflect.Type) []string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == timeType {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(fixture, &obj); err != nil || obj == nil {
			return []string{pathOrRoot(path) + " (fixture is not an object)"}
		}
		var out []string
		for i := 0; i < typ.NumField(); i++ {
			name, omitEmpty, ok := jsonTagName(typ.Field(i))
			if !ok {
				continue
			}
			child := name
			if path != "" {
				child = path + "." + name
			}
			value, present := obj[name]
			if !present {
				if !omitEmpty {
					out = append(out, child)
				}
				continue
			}
			out = append(out, walkUncovered(child, value, typ.Field(i).Type)...)
		}
		return out
	case reflect.Slice, reflect.Array:
		var items []json.RawMessage
		if err := json.Unmarshal(fixture, &items); err != nil {
			return []string{pathOrRoot(path) + " (fixture is not an array)"}
		}
		if len(items) == 0 {
			if needsKeys(typ.Elem()) {
				return []string{path + "[0] (empty array)"}
			}
			return nil
		}
		return walkUncovered(path+"[0]", items[0], typ.Elem())
	case reflect.Map:
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(fixture, &entries); err != nil {
			return []string{pathOrRoot(path) + " (fixture is not an object)"}
		}
		if len(entries) == 0 {
			if needsKeys(typ.Elem()) {
				return []string{path + "{} (empty map)"}
			}
			return nil
		}
		keys := make([]string, 0, len(entries))
		for k := range entries {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var out []string
		for _, k := range keys {
			out = append(out, walkUncovered(path+"."+k, entries[k], typ.Elem())...)
		}
		return out
	}
	return nil
}

func needsKeys(typ reflect.Type) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ.Kind() == reflect.Struct && typ != timeType
}

func pathOrRoot(path string) string {
	if path == "" {
		return "$"
	}
	return path
}

func assertFixtureDecodes(t *testing.T, name string, out any) {
	t.Helper()
	data := loadContractFixture(t, name)
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	for _, path := range uncoveredKeys(data, reflect.TypeOf(out).Elem()) {
		t.Errorf("%s does not cover %s", name, path)
	}
}

type uncoveredProbe struct {
	Name   string      `json:"name"`
	Skip   string      `json:"-"`
	Opt    string      `json:"opt,omitempty"`
	Detail probeDetail `json:"detail"`
}

type probeDetail struct {
	Count     int       `json:"count"`
	CheckedAt time.Time `json:"checked_at"`
}

type probeOne struct {
	HeapMB uint64 `json:"heap_mb"`
}

type probeSlice struct {
	Items []probeDetail `json:"items"`
}

func TestUncoveredKeys(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		typ     reflect.Type
		want    []string
	}{
		{"full fixture", `{"name":"a","opt":"b","detail":{"count":1,"checked_at":"2026-01-01T00:00:00Z"}}`, reflect.TypeOf(uncoveredProbe{}), nil},
		{"one-field struct matching key", `{"heap_mb":1}`, reflect.TypeOf(probeOne{}), nil},
		{"look-alike key refused", `{"heapMB":1}`, reflect.TypeOf(probeOne{}), []string{"heap_mb"}},
		{"omitempty absent", `{"name":"a","detail":{"count":1,"checked_at":"2026-01-01T00:00:00Z"}}`, reflect.TypeOf(uncoveredProbe{}), nil},
		{"nested non-omitempty absent", `{"name":"a","detail":{"count":1}}`, reflect.TypeOf(uncoveredProbe{}), []string{"detail.checked_at"}},
		{"slice with empty array", `{"items":[]}`, reflect.TypeOf(probeSlice{}), []string{"items[0] (empty array)"}},
		{"slice element checked", `{"items":[{"count":1}]}`, reflect.TypeOf(probeSlice{}), []string{"items[0].checked_at"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := uncoveredKeys(json.RawMessage(tc.fixture), tc.typ)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("uncoveredKeys = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestContract_HealthFixtureDecodesIntoOperatorHealth(t *testing.T) {
	var got OperatorHealth
	assertFixtureDecodes(t, "observe_health.json", &got)
	if got.DB != "down" {
		t.Errorf("DB = %q, want down", got.DB)
	}
	if got.Detail.DBError != "db refused" {
		t.Errorf("Detail.DBError = %q, want db refused", got.Detail.DBError)
	}
	if got.Detail.CheckedAt.IsZero() {
		t.Error("Detail.CheckedAt is zero")
	}
	if got.Healthy() {
		t.Error("Healthy() = true, want false")
	}
}
