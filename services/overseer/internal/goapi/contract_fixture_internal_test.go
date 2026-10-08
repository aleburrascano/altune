package goapi

import (
	"bytes"
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
	return walkUncovered("", fixture, typ, false)
}

func uncoveredKeysStrict(fixture json.RawMessage, typ reflect.Type) []string {
	return walkUncovered("", fixture, typ, true)
}

func walkUncovered(path string, fixture json.RawMessage, typ reflect.Type, strict bool) []string {
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
				if !omitEmpty || strict {
					out = append(out, child)
				}
				continue
			}
			out = append(out, walkUncovered(child, value, typ.Field(i).Type, strict)...)
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
		return walkUncovered(path+"[0]", items[0], typ.Elem(), strict)
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
			out = append(out, walkUncovered(path+"."+k, entries[k], typ.Elem(), strict)...)
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

func decodeFixtureStrict(t *testing.T, name string, out any, exempt ...string) {
	t.Helper()
	data := loadContractFixture(t, name)
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	skip := map[string]bool{}
	for _, p := range exempt {
		skip[p] = true
	}
	for _, path := range uncoveredKeysStrict(data, reflect.TypeOf(out).Elem()) {
		if !skip[path] {
			t.Errorf("%s does not cover %s", name, path)
		}
	}
}

func TestContract_MetricsLiveFixtureDecodes(t *testing.T) {
	var live LiveMetrics
	decodeFixtureStrict(t, "observe_metrics_live.json", &live)
	if got := live.Latency.Routes["/v1/tracks/{trackId}"].Count; got < 1 {
		t.Errorf("track route count = %d, want >= 1", got)
	}
	var usage providerUsageEnvelope
	decodeFixtureStrict(t, "observe_metrics_live.json", &usage)
	if len(usage.Providers) == 0 {
		t.Error("Providers is empty")
	}
}

func TestContract_EvalFixtureDecodes(t *testing.T) {
	var got EvalStatus
	decodeFixtureStrict(t, "observe_eval.json", &got, "error")
	if !got.Scored() {
		t.Error("Scored() = false, want true")
	}
	if got.Baseline == nil {
		t.Error("Baseline is nil")
	}
	if got.LastRun == nil {
		t.Error("LastRun is nil")
	}
	if len(got.Queries) != 1 {
		t.Errorf("len(Queries) = %d, want 1", len(got.Queries))
	}
}

func TestContract_EvalFixtureLookAlikeIsRefused(t *testing.T) {
	data := loadContractFixture(t, "observe_eval.json")
	lookAlike := strings.Replace(string(data), `"queries"`, `"query"`, 1)
	if lookAlike == string(data) {
		t.Fatal("fixture has no queries key to rename")
	}
	got := uncoveredKeysStrict(json.RawMessage(lookAlike), reflect.TypeOf(EvalStatus{}))
	want := []string{"error", "queries"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("uncoveredKeysStrict = %q, want %q", got, want)
	}
}

func TestContract_AcquisitionFixtureDecodes(t *testing.T) {
	var got AcquisitionStatus
	decodeFixtureStrict(t, "observe_acquisition.json", &got)
	if got.InFlight == 0 || got.Succeeded == 0 || got.Failed == 0 || got.Rejected == 0 ||
		got.QueueDepth == 0 || got.QueueCapacity == 0 {
		t.Errorf("a counter is zero: %+v", got)
	}
}

func TestContract_QualityFixtureDecodes(t *testing.T) {
	var got DiscographyQuality
	decodeFixtureStrict(t, "observe_quality_discography.json", &got)
	if len(got.Cases) != 1 {
		t.Fatalf("len(Cases) = %d, want 1", len(got.Cases))
	}
	if len(got.Cases[0].ProviderCounts) == 0 {
		t.Error("ProviderCounts is empty")
	}
}

func sseFrame(t *testing.T, name string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, loadContractFixture(t, name)); err != nil {
		t.Fatalf("compact %s: %v", name, err)
	}
	return "data: " + buf.String() + "\n\n"
}

func TestContract_EventFrameFixtureDecodes(t *testing.T) {
	var got Event
	assertFixtureDecodes(t, "observe_events_frame.json", &got)
	assertFixtureStrictlyCovered(t, "observe_events_frame.json", reflect.TypeOf(got))
	assertEventFields(t, got)

	viaSSE, err := newSSEDecoder(strings.NewReader(sseFrame(t, "observe_events_frame.json"))).next()
	if err != nil {
		t.Fatalf("sseDecoder.next: %v", err)
	}
	assertEventFields(t, viaSSE)
	if !viaSSE.Timestamp.Equal(got.Timestamp) || viaSSE != got {
		t.Errorf("sseDecoder event = %+v, want %+v", viaSSE, got)
	}
}

func assertFixtureStrictlyCovered(t *testing.T, name string, typ reflect.Type) {
	t.Helper()
	for _, path := range uncoveredKeysStrict(loadContractFixture(t, name), typ) {
		t.Errorf("%s does not carry %s", name, path)
	}
}

func assertEventFields(t *testing.T, ev Event) {
	t.Helper()
	if ev.Type == "" {
		t.Error("Type is empty")
	}
	if ev.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if ev.CorrID != "contract-corr" {
		t.Errorf("CorrID = %q, want contract-corr", ev.CorrID)
	}
}

func assertLogFields(t *testing.T, rec LogRecord) {
	t.Helper()
	if rec.Level != "INFO" {
		t.Errorf("Level = %q, want INFO", rec.Level)
	}
	if rec.Message == "" {
		t.Error("Message is empty")
	}
	if rec.Fields["stage"] != "resolve" {
		t.Errorf("Fields[stage] = %q, want resolve", rec.Fields["stage"])
	}
}

func TestContract_LogFrameFixtureDecodes(t *testing.T) {
	var got LogRecord
	assertFixtureDecodes(t, "observe_logs_frame.json", &got)
	assertLogFields(t, got)

	viaSSE, err := newLogSSEDecoder(strings.NewReader(sseFrame(t, "observe_logs_frame.json"))).next()
	if err != nil {
		t.Fatalf("logSSEDecoder.next: %v", err)
	}
	assertLogFields(t, viaSSE)
	if !viaSSE.Time.Equal(got.Time) || viaSSE.Message != got.Message {
		t.Errorf("logSSEDecoder record = %+v, want %+v", viaSSE, got)
	}
}

func TestContract_LogFrameLookAlikeIsRefused(t *testing.T) {
	data := loadContractFixture(t, "observe_logs_frame.json")
	lookAlike := strings.Replace(string(data), `"msg"`, `"message"`, 1)
	if lookAlike == string(data) {
		t.Fatal("fixture has no msg key to rename")
	}
	got := uncoveredKeys(json.RawMessage(lookAlike), reflect.TypeOf(LogRecord{}))
	if !reflect.DeepEqual(got, []string{"msg"}) {
		t.Errorf("uncoveredKeys = %q, want [msg]", got)
	}
}
