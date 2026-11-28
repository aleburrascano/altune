package domain_test

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/music"
	"reflect"
	"testing"
)

var identityFields = map[string]string{
	"Title":            "the recording's title",
	"Subtitle":         "the credited artist of a track",
	"Album":            "which release it sits on",
	"Year":             "release year tells reissues apart",
	"ReleaseDate":      "release date tells reissues apart",
	"RecordType":       "single, album or compilation",
	"ISRC":             "the recording code",
	"RecordingMBID":    "the recording id",
	"ReleaseMBID":      "the release id",
	"ReleaseGroupMBID": "the release group id",
	"ArtistMBID":       "the artist id",
	"UPC":              "the release barcode",
	"DeezerAlbumID":    "the provider's release id",
	"TrackPosition":    "disc and track on the release",
	"TrackCount":       "size of the release",
	"Contributors":     "credited artists and featured artists",
	"Explicit":         "explicit or clean version",
	"Duration":         "the recording's length",
	"Sources":          "where the audio can be fetched",
	"Xref":             "ids of the same entity in other catalogs",
}

var notIdentityFields = map[string]string{
	"Kind":           "selects which results qualify; the identity describes a track",
	"ImageURL":       "artwork, not identity",
	"ArtworkSource":  "artwork provenance, not identity",
	"ArtworkPath":    "artwork provenance, not identity",
	"Confidence":     "ranking signal, not identity",
	"Popularity":     "ranking signal, not identity",
	"ProviderRank":   "ranking signal, not identity",
	"FanCount":       "ranking signal, not identity",
	"Signature":      "dedupe key derived from the fields above",
	"ResolutionTier": "pipeline provenance, not identity",
	"Extras":         "provider-specific bag, not identity",
}

func TestSearchResult_EveryFieldIsClassifiedAsIdentityOrNot(t *testing.T) {
	typ := reflect.TypeFor[domain.SearchResult]()
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		_, identity := identityFields[name]
		_, other := notIdentityFields[name]
		if identity == other {
			t.Errorf("SearchResult.%s must be listed in exactly one of identityFields and notIdentityFields", name)
		}
	}
	for name := range identityFields {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("identityFields lists %s, which SearchResult no longer has", name)
		}
	}
	for name := range notIdentityFields {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("notIdentityFields lists %s, which SearchResult no longer has", name)
		}
	}
}

func TestSearchResult_RecordingIdentityCarriesEveryIdentityFieldAndNoOther(t *testing.T) {
	full := filled[domain.SearchResult]()
	want := full.RecordingIdentity()
	for name := range identityFields {
		if reflect.DeepEqual(withFieldCleared(full, name).RecordingIdentity(), want) {
			t.Errorf("clearing SearchResult.%s left the recording identity unchanged, want it carried", name)
		}
	}
	for name := range notIdentityFields {
		if !reflect.DeepEqual(withFieldCleared(full, name).RecordingIdentity(), want) {
			t.Errorf("clearing SearchResult.%s changed the recording identity, want it left out", name)
		}
	}
}

func TestSearchResult_RecordingIdentityFillsEveryIdentityField(t *testing.T) {
	got := reflect.ValueOf(filled[domain.SearchResult]().RecordingIdentity())
	for i := range got.NumField() {
		if got.Field(i).IsZero() {
			t.Errorf("RecordingIdentity.%s stays empty for a fully populated result, want a field that search fills", got.Type().Field(i).Name)
		}
	}
}

func TestSearchResult_RecordingIdentityDoesNotShareItsExplicitFlagWithTheResult(t *testing.T) {
	flag := true
	result := domain.SearchResult{Explicit: &flag}

	identity := result.RecordingIdentity()
	*identity.Explicit = false

	if !flag {
		t.Error("changing the identity's explicit flag changed the search result, want a copy")
	}
}

func TestSearchResult_RecordingIdentityOfABareResultIsTheZeroValue(t *testing.T) {
	identity := domain.SearchResult{}.RecordingIdentity()

	if !reflect.DeepEqual(identity, music.RecordingIdentity{}) {
		t.Errorf("identity of an empty result = %+v, want the zero value", identity)
	}
}

func withFieldCleared(r domain.SearchResult, name string) domain.SearchResult {
	v := reflect.ValueOf(&r).Elem()
	field := v.FieldByName(name)
	field.Set(reflect.Zero(field.Type()))
	return r
}

func filled[T any]() T {
	var value T
	fill(reflect.ValueOf(&value).Elem(), "v")
	return value
}

func fill(v reflect.Value, name string) {
	switch v.Kind() {
	case reflect.String:
		v.SetString(name)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(v.Elem(), name)
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fill(v.Index(0), name)
	case reflect.Map:
		v.Set(reflect.MakeMap(v.Type()))
		key := reflect.New(v.Type().Key()).Elem()
		fill(key, name)
		elem := reflect.New(v.Type().Elem()).Elem()
		fill(elem, name)
		v.SetMapIndex(key, elem)
	case reflect.Interface:
		v.Set(reflect.ValueOf(name))
	case reflect.Struct:
		for i := range v.NumField() {
			fill(v.Field(i), v.Type().Field(i).Name)
		}
	case reflect.Invalid, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Complex64, reflect.Complex128, reflect.Array, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		panic("fill: unsupported kind " + v.Kind().String())
	}
}
