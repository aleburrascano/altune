package providerfields_test

import (
	"altune/go-api/cmd/providerschema/internal/providerfields"
	"altune/go-api/cmd/providerschema/internal/providerfields/internal/core/schema"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func observe(t *testing.T, raws ...string) providerfields.Observed {
	t.Helper()
	into := providerfields.Observed{}
	for _, raw := range raws {
		if err := providerfields.Observe([]byte(raw), into); err != nil {
			t.Fatal(err)
		}
	}
	return into
}

func TestObserveListsEveryPathWithItsType(t *testing.T) {
	got := observe(t, `{"id":"1","attributes":{"name":"x","genreNames":["a"],"previews":[{"url":"u"}],"n":2,"ok":true,"gone":null}}`)
	want := providerfields.Observed{
		"id": "string", "attributes": "object", "attributes.name": "string",
		"attributes.genreNames": "array", "attributes.genreNames[]": "string",
		"attributes.previews": "array", "attributes.previews[].url": "string",
		"attributes.n": "number", "attributes.ok": "boolean", "attributes.gone": "null",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestObserveUnionsFieldsAndTypesAcrossSamples(t *testing.T) {
	got := observe(t, `{"a":"x"}`, `{"a":null,"b":1}`, `{"a":"y"}`)
	want := providerfields.Observed{"a": "null|string", "b": "number"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestObserveRejectsAPayloadThatIsNotAnObject(t *testing.T) {
	if err := providerfields.Observe([]byte(`[1]`), providerfields.Observed{}); err == nil {
		t.Fatal("want an error")
	}
}

func TestMergeKeepsReasonsAndRecomputesWhereEachFieldWasSeen(t *testing.T) {
	e := providerfields.Entity{Fields: []providerfields.Field{
		{Path: "old", Type: "string", Seen: []string{"live"}, Unused: "kept for history"},
		{Path: "name", Type: "string", Seen: []string{"live"}, Read: true},
	}}
	e.Merge(providerfields.Observed{"name": "string", "rating": "string"},
		providerfields.Observed{"name": "string", "extra": "boolean", "rating": "null"})
	want := []providerfields.Field{
		{Path: "extra", Type: "boolean", Seen: []string{"live"}},
		{Path: "name", Type: "string", Seen: []string{"reference", "live"}, Read: true},
		{Path: "old", Type: "string", Unused: "kept for history"},
		{Path: "rating", Type: "string", Seen: []string{"reference", "live"}},
	}
	if !reflect.DeepEqual(e.Fields, want) {
		t.Fatalf("got %+v\nwant %+v", e.Fields, want)
	}
}

func TestMergeTakesTheLiveTypeWhenTheReferenceIsSilent(t *testing.T) {
	e := providerfields.Entity{Fields: []providerfields.Field{{Path: "traits", Type: "string", Seen: []string{"live"}}}}
	e.Merge(providerfields.Observed{}, providerfields.Observed{"traits": "array"})
	if e.Fields[0].Type != "array" {
		t.Fatalf("got %+v", e.Fields[0])
	}
}

const shapeSource = `package shape

type art struct {
	URL string ` + "`json:\"url\"`" + `
}

type decodeShape struct {
	ID         string ` + "`json:\"id,omitempty\"`" + `
	Skipped    string ` + "`json:\"-\"`" + `
	hidden     string
	Untagged   int
	Attributes *struct {
		Artwork  art      ` + "`json:\"artwork\"`" + `
		Genres   []string ` + "`json:\"genreNames\"`" + `
		Previews []*art   ` + "`json:\"previews\"`" + `
	} ` + "`json:\"attributes\"`" + `
	Extras map[string]any    ` + "`json:\"extras\"`" + `
	Items  []any             ` + "`json:\"items\"`" + `
	Raw    json.RawMessage   ` + "`json:\"raw\"`" + `
	Loose  interface{}       ` + "`json:\"loose\"`" + `
	ByID   map[string]art    ` + "`json:\"byID\"`" + `
	Counts map[string]int    ` + "`json:\"counts\"`" + `
}

type page[T any] struct {
	Items []T ` + "`json:\"items\"`" + `
}

func fetchList[T any]() {
	var body struct {
		Collection []T ` + "`json:\"collection\"`" + `
	}
	_ = body
}

type node struct {
	Name     string ` + "`json:\"name\"`" + `
	Children []node ` + "`json:\"children\"`" + `
}

type withEmbedded struct {
	art
	*node
	Named art ` + "`json:\"named\"`" + `
	tagged ` + "`json:\"tagged\"`" + `
}

type tagged struct {
	Label string ` + "`json:\"label\"`" + `
}

type grid struct {
	Rows [][]art ` + "`json:\"rows\"`" + `
}

type tagList struct {
	Tag []struct {
		Name string ` + "`json:\"name\"`" + `
	} ` + "`json:\"tag\"`" + `
}

type lookupPath []any

type notAPath struct{ X string }

func parse() {
	var body struct {
		Total int ` + "`json:\"total\"`" + `
	}
	_ = body
}

func (decodeShape) twice() {
	var dup struct{ A string }
	_ = dup
}

func (art) twice() {
	var dup struct{ B string }
	_ = dup
}

func use(m map[string]any) {
	_ = m["name"]
	_ = lookupPath{"inner", 0, "label"}
	_ = notAPath{"other"}
	m["written"] = 1
}
`

func shapeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"shape.go":      shapeSource,
		"shape_test.go": "package shape\n\ntype testOnly struct {\n\tLeak string\n}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDecodedFieldsListsThePathsAStructInGoSourceReads(t *testing.T) {
	got, err := providerfields.DecodedFields([]string{shapeDir(t)}, "decodeShape")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"extras": true, "items": true, "items[]": true, "raw": true, "loose": true,
		"byID": true, "byID.*": true, "byID.*.url": true, "counts": true, "counts.*": true,
		"id": true, "Untagged": true, "attributes": true,
		"attributes.artwork": true, "attributes.artwork.url": true,
		"attributes.genreNames": true, "attributes.genreNames[]": true,
		"attributes.previews": true, "attributes.previews[].url": true,
	}
	if !reflect.DeepEqual(got.Paths, want) {
		t.Fatalf("got %v\nwant %v", got.Paths, want)
	}
}

func TestDecodedFieldsReadsKeysTheAdapterLooksUpUnderOpaqueFields(t *testing.T) {
	got, err := providerfields.DecodedFields([]string{shapeDir(t)}, "decodeShape")
	if err != nil {
		t.Fatal(err)
	}
	read := map[string]bool{
		"extras.name": true, "items[].name": true, "raw.inner.label": true, "loose.name": true,
		"extras.written": false, "extras.other": false, "attributes.name": false, "listed.label": false,
	}
	for path, want := range read {
		if got.Has(path) != want {
			t.Errorf("Has(%q) = %v, want %v", path, !want, want)
		}
	}
}

func TestDecodedFieldsWithNoTypeTreatsTheWholeResponseAsOpaque(t *testing.T) {
	got, err := providerfields.DecodedFields([]string{shapeDir(t)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Has("deep.down.name") || !got.Has("a[].label") || got.Has("a.unknown") || len(got.Paths) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestDecodedFieldsFindsAStructDeclaredInsideAFunction(t *testing.T) {
	got, err := providerfields.DecodedFields([]string{shapeDir(t)}, "parse.body")
	if err != nil || !reflect.DeepEqual(got.Paths, map[string]bool{"total": true}) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDecodedFieldsRejectsANameTwoFunctionsShare(t *testing.T) {
	if _, err := providerfields.DecodedFields([]string{shapeDir(t)}, "twice.dup"); err == nil {
		t.Fatal("want an error for an ambiguous name")
	}
}

func TestDecodedFieldsIgnoresTestFiles(t *testing.T) {
	if _, err := providerfields.DecodedFields([]string{shapeDir(t)}, "testOnly"); err == nil {
		t.Fatal("a struct from a _test.go file was used")
	}
}

func TestDecodedFieldsFailsOnAFolderWithNoGoSource(t *testing.T) {
	if _, err := providerfields.DecodedFields([]string{t.TempDir()}, "decodeShape"); err == nil {
		t.Fatal("want an error")
	}
}

func TestAccountMarksWhatTheAdapterReads(t *testing.T) {
	e := providerfields.Entity{Fields: []providerfields.Field{{Path: "a"}, {Path: "b", Read: true}}}
	e.Account(providerfields.Reads{Paths: map[string]bool{"a": true, "0new": true}})
	want := []providerfields.Field{{Path: "0new", Type: schema.TypeUnseen, Read: true}, {Path: "a", Read: true}, {Path: "b"}}
	if !reflect.DeepEqual(e.Fields, want) {
		t.Fatalf("got %+v", e.Fields)
	}
}

func TestAuditNamesEveryFieldThatIsNotAccountedFor(t *testing.T) {
	live := []string{"live"}
	e := providerfields.Entity{Fields: []providerfields.Field{
		{Path: "fine", Seen: live, Read: true},
		{Path: "reason", Seen: live, Unused: "display only"},
		{Path: "gone", Read: true},
		{Path: "stale", Seen: live},
		{Path: "silent", Seen: live},
		{Path: "both", Seen: live, Read: true, Unused: "x"},
		{Path: "absent", Read: true, Absent: "only the lookup sends it"},
		{Path: "unexplained", Read: true},
		{Path: "back", Seen: live, Read: true, Absent: "x"},
	}}
	got := e.Audit("song", providerfields.Reads{Paths: map[string]bool{"fine": true, "stale": true, "both": true, "extra": true, "absent": true, "unexplained": true, "back": true}})
	want := []string{
		"song.back: the response sends it now, so drop its absent reason",
		"song.both: the adapter reads it, so drop its unused reason",
		"song.extra: the adapter decodes it but the schema does not list it",
		"song.gone: neither the reference nor a live probe shows it any more",
		"song.silent: the adapter does not read it and no unused reason is written",
		"song.stale: the read flag disagrees with the adapter (run: go test ./cmd/providerschema -update-schema)",
		"song.unexplained: the adapter decodes it but this response never sends it, so write an absent reason or stop decoding it",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestSaveWritesSortedFieldsThatLoadReadsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.yaml")
	in := providerfields.Schema{Provider: "p", Reference: "r", Entities: map[string]*providerfields.Entity{
		"song": {
			Fixtures: []schema.Fixture{{ID: "1", Why: "plain"}},
			Fields:   []providerfields.Field{{Path: "b", Type: "string", Seen: []string{"live"}}, {Path: "a", Type: "number", Seen: []string{"live"}, Read: true}},
		},
	}}
	if err := providerfields.Save(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := providerfields.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	song := out.Entities["song"]
	if song == nil {
		t.Fatal("the round trip lost the song entity")
	}
	if got := song.Fields; got[0].Path != "a" || got[1].Path != "b" || !reflect.DeepEqual(out, in) {
		t.Fatalf("round trip changed it: %+v", out)
	}
}

func TestLoadRejectsAKeyTheSchemaDoesNotDefine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.yaml")
	if err := os.WriteFile(path, []byte("provider: p\ntypo: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := providerfields.Load(path); err == nil || !strings.Contains(err.Error(), "typo") {
		t.Fatalf("want an error naming the key, got %v", err)
	}
}

func TestLoadRejectsAnEmptyEntity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.yaml")
	if err := os.WriteFile(path, []byte("provider: p\nentities:\n  track:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := providerfields.Load(path); err == nil || !strings.Contains(err.Error(), "entity track is empty") {
		t.Fatalf("want an error naming the empty entity, got %v", err)
	}
}

func TestLoadFailsLoudlyOnAMissingFile(t *testing.T) {
	if _, err := providerfields.Load(filepath.Join(t.TempDir(), "none.yaml")); err == nil {
		t.Fatal("want an error")
	}
}

func TestDecodedFieldsListsIDKeyedMapsAndCollapseFoldsTheirKeys(t *testing.T) {
	got, err := providerfields.DecodedFields([]string{shapeDir(t)}, "decodeShape")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Maps, []string{"byID", "counts"}) {
		t.Fatalf("maps %v", got.Maps)
	}
	live := observe(t, `{"byID":{"a1":{"url":"u"},"b2":{"url":null,"art":[{"x":1}]}},"counts":{"z":1},"other":{"k":"v"}}`)
	want := providerfields.Observed{
		"byID": "object", "byID.*": "object", "byID.*.url": "null|string", "byID.*.art": "array", "byID.*.art[].x": "number",
		"counts": "object", "counts.*": "number", "other": "object", "other.k": "string",
	}
	if collapsed := live.Collapse(got.Maps); !reflect.DeepEqual(collapsed, want) {
		t.Fatalf("got %v", collapsed)
	}
}

func TestGraftReadsARawFieldThroughTheStructItIsDecodedIntoLater(t *testing.T) {
	outer, err := providerfields.DecodedFields([]string{shapeDir(t)}, "decodeShape")
	if err != nil {
		t.Fatal(err)
	}
	inner, err := providerfields.DecodedFields([]string{shapeDir(t)}, "tagList")
	if err != nil {
		t.Fatal(err)
	}
	got, err := outer.Graft("raw", inner)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Has("raw.tag[].name") || !got.Has("raw.tag") || got.Has("raw.inner.label") || !got.Has("extras.name") || !outer.Match("raw.tag[].name").ByKey || got.Match("raw.tag[].name").ByKey {
		t.Fatalf("got %+v", got)
	}
	if _, err := outer.Graft("attributes", inner); err == nil {
		t.Fatal("want an error grafting onto a field that is not raw")
	}
}

func TestAccountMarksFieldsReadOnlyByKeyMatch(t *testing.T) {
	reads, err := providerfields.DecodedFields([]string{shapeDir(t)}, "")
	if err != nil {
		t.Fatal(err)
	}
	e := providerfields.Entity{Fields: []providerfields.Field{{Path: "x.name", Seen: []string{"live"}, Unused: "a menu label the adapter never walks"}, {Path: "x.other", Seen: []string{"live"}, Unused: "u"}}}
	e.Account(reads)
	if !e.Fields[0].Read || !e.Fields[0].ByKey || e.Fields[1].Read || e.Fields[1].ByKey {
		t.Fatalf("got %+v", e.Fields)
	}
	if problems := e.Audit("s", reads); len(problems) != 0 {
		t.Fatalf("got %v", problems)
	}
	e.Fields[0].ByKey = false
	if problems := e.Audit("s", reads); len(problems) != 1 {
		t.Fatalf("a stale by_key flag passed: %v", problems)
	}
}

func TestDecodedFieldsReadsStructsAcrossSeveralFolders(t *testing.T) {
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "more.go"), []byte("package more\n\ntype elsewhere struct {\n\tName string `json:\"name\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := providerfields.DecodedFields([]string{shapeDir(t), other}, "elsewhere")
	if err != nil || !got.Has("name") {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := providerfields.DecodedFields([]string{shapeDir(t), t.TempDir()}, "elsewhere"); err == nil {
		t.Fatal("want an error for a folder with no Go source")
	}
	if _, err := providerfields.DecodedFields(nil, "elsewhere"); err == nil {
		t.Fatal("want an error for no folders")
	}
}

func TestDecodedFieldsBindsTypeArgumentsOfAGenericDecodeStruct(t *testing.T) {
	for typeName, want := range map[string]map[string]bool{
		"fetchList.body[art]": {"collection": true, "collection[].url": true},
		"page[art]":           {"items": true, "items[].url": true},
	} {
		got, err := providerfields.DecodedFields([]string{shapeDir(t)}, typeName)
		if err != nil || !reflect.DeepEqual(got.Paths, want) {
			t.Errorf("%s: got %v, %v", typeName, got.Paths, err)
		}
	}
	if _, err := providerfields.DecodedFields([]string{shapeDir(t)}, "fetchList.body"); err == nil {
		t.Fatal("want an error when a type argument is missing")
	}
}

func TestDecodedFieldsWalksAStructThatContainsItselfOneLevelDeep(t *testing.T) {
	got, err := providerfields.DecodedFields([]string{shapeDir(t)}, "node")
	want := map[string]bool{"name": true, "children": true, "children[].name": true, "children[].children": true}
	if err != nil || !reflect.DeepEqual(got.Paths, want) {
		t.Fatalf("got %v, %v", got.Paths, err)
	}
}

func TestDecodedFieldsPromotesTheFieldsOfAnEmbeddedStruct(t *testing.T) {
	got, err := providerfields.DecodedFields([]string{shapeDir(t)}, "withEmbedded")
	want := map[string]bool{
		"url": true, "name": true, "children": true, "children[].name": true, "children[].children": true,
		"named": true, "named.url": true, "tagged": true, "tagged.label": true,
	}
	if err != nil || !reflect.DeepEqual(got.Paths, want) {
		t.Fatalf("got %v, %v", got.Paths, err)
	}
}

func TestCollapseFoldsAnOuterMapBeforeAnInnerOne(t *testing.T) {
	live := observe(t, `{"a":{"x":{"b":{"y":{"c":1}}}},"m":{"":{"k":1}}}`)
	got := live.Collapse([]string{"a.*.b", "a", "m"})
	for _, path := range []string{"a.*.b.*.c", "m.*.k"} {
		if _, ok := got[path]; !ok {
			t.Errorf("no %s in %v", path, got)
		}
	}
}

func TestGraftWorksOnTheFirstRawField(t *testing.T) {
	outer, err := providerfields.DecodedFields([]string{shapeDir(t)}, "decodeShape")
	if err != nil {
		t.Fatal(err)
	}
	inner, err := providerfields.DecodedFields([]string{shapeDir(t)}, "tagList")
	if err != nil {
		t.Fatal(err)
	}
	if outer.Opaque[0] != "extras" {
		t.Fatalf("raw fields %v", outer.Opaque)
	}
	got, err := outer.Graft("extras", inner)
	if err != nil || !got.Has("extras.tag[].name") {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestNestedArraysAreObservedAndDecodedAllTheWayDown(t *testing.T) {
	live := observe(t, `{"rows":[[{"url":"u","secret":1}],[2]]}`)
	want := providerfields.Observed{"rows": "array", "rows[]": "array", "rows[][].url": "string", "rows[][].secret": "number", "rows[][]": "number"}
	if !reflect.DeepEqual(live, want) {
		t.Fatalf("observed %v", live)
	}
	got, err := providerfields.DecodedFields([]string{shapeDir(t)}, "grid")
	if err != nil || !reflect.DeepEqual(got.Paths, map[string]bool{"rows": true, "rows[]": true, "rows[][].url": true}) {
		t.Fatalf("decoded %v, %v", got.Paths, err)
	}
}
