package evidence_test

import (
	"altune/go-api/internal/download/internal/adapters/evidence"
	"altune/go-api/internal/download/internal/domain"
	"encoding/json"
	"reflect"
	"testing"
)

func TestCodec_EncodeKeepsTheStoredJSONBytes(t *testing.T) {
	delta := 1.5
	full := domain.AcquisitionEvidence{
		Verdict:              "hard",
		AcoustIDScore:        0.93,
		DurationDeltaSeconds: &delta,
		Channel:              "Artist - Topic",
		Qualifiers:           []string{"live"},
		Resolved:             true,
		SourceTitle:          "Song",
		ISRCSource:           domain.ProofCatalog,
		SourceKind:           domain.ArtTrack,
		PipelineGeneration:   3,
		Grade:                domain.GradeA,
		SourceFormatID:       "251",
		SourceKbps:           128,
	}
	cases := map[string]struct {
		in   domain.AcquisitionEvidence
		want string
	}{
		"every key":                        {full, `{"verdict":"hard","acoustid_score":0.93,"duration_delta_seconds":1.5,"channel":"Artist - Topic","qualifiers":["live"],"resolved":true,"source_title":"Song","isrc_source":"catalog","source_kind":"art_track","pipeline_generation":3,"grade":"A","source_format_id":"251","source_kbps":128}`},
		"zero value omits the format keys": {domain.AcquisitionEvidence{}, `{"verdict":"","acoustid_score":0,"duration_delta_seconds":null,"channel":"","qualifiers":null,"resolved":false,"source_title":"","isrc_source":"","source_kind":"","pipeline_generation":0,"grade":""}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := evidence.NewCodec().Encode(tc.in)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if string(raw) != tc.want {
				t.Fatalf("stored bytes changed\n got %s\nwant %s", raw, tc.want)
			}
		})
	}
}

func TestCodec_DecodeReturnsWhatEncodeStored(t *testing.T) {
	delta := 2.0
	in := domain.AcquisitionEvidence{Verdict: "soft", DurationDeltaSeconds: &delta, Qualifiers: []string{"remix"}, Grade: domain.GradeB, SourceKbps: 96}
	codec := evidence.NewCodec()
	raw, err := codec.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := codec.Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !reflect.DeepEqual(in, got) {
		t.Fatalf("round trip changed the evidence: got %+v want %+v", got, in)
	}
}

func TestCodec_DecodeRefusesBadJSON(t *testing.T) {
	if _, err := evidence.NewCodec().Decode(json.RawMessage(`{"verdict":`)); err == nil {
		t.Fatal("Decode accepted truncated JSON")
	}
}

func TestCodec_DecodeInPlaceReadsTheStoredKeys(t *testing.T) {
	raw := json.RawMessage(`{"grade":"B","source_kind":"art_track","in_place_generation":2,"source_format_id":"139","source_kbps":48,"verdict":"ignored"}`)
	got, err := evidence.NewCodec().DecodeInPlace(raw)
	if err != nil {
		t.Fatalf("DecodeInPlace: %v", err)
	}
	want := domain.InPlaceEvidence{Grade: domain.GradeB, SourceKind: domain.ArtTrack, InPlaceGeneration: 2, SourceFormatID: "139", SourceKbps: 48}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if format := got.Format(); format.ID != "139" || format.Kbps != 48 {
		t.Fatalf("Format() = %+v", format)
	}
}

func TestCodec_DecodeInPlaceRefusesBadJSON(t *testing.T) {
	if _, err := evidence.NewCodec().DecodeInPlace(json.RawMessage(`nope`)); err == nil {
		t.Fatal("DecodeInPlace accepted bad JSON")
	}
}
