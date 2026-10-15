package goldeneval

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/fanout"
	"altune/go-api/internal/shared/songfacts"
	"altune/go-api/internal/shared/versiontag"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	discoveryEval "altune/go-api/internal/discovery/service/eval"
)

const defaultWithin = 3

var (
	mbidForm         = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	knownFailingForm = regexp.MustCompile(`^#\d+$`)
	scoredKinds      = []domain.ResultKind{domain.ResultKindTrack, domain.ResultKindAlbum, domain.ResultKindArtist}
)

type Case struct {
	Query        string   `json:"query"`
	Kind         string   `json:"kind"`
	Artist       string   `json:"artist"`
	Title        string   `json:"title"`
	ISRCs        []string `json:"isrcs,omitempty"`
	MBID         string   `json:"mbid,omitempty"`
	Labels       []string `json:"labels"`
	Within       int      `json:"within,omitempty"`
	KnownFailing string   `json:"known_failing,omitempty"`

	kind domain.ResultKind
}

func IsMBID(s string) bool { return mbidForm.MatchString(s) }

func Parse(goldensJSON []byte) ([]Case, error) {
	var cases []Case
	if err := json.Unmarshal(goldensJSON, &cases); err != nil {
		return nil, fmt.Errorf("parse goldens: %w", err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("goldens: no cases")
	}
	seen := map[string]bool{}
	for i := range cases {
		if err := cases[i].normalize(); err != nil {
			return nil, fmt.Errorf("golden %d (%q): %w", i, cases[i].Query, err)
		}
		key := cases[i].Query + "|" + cases[i].Kind + "|" + cases[i].identity()
		if seen[key] {
			return nil, fmt.Errorf("golden %d (%q): duplicate of an earlier case", i, cases[i].Query)
		}
		seen[key] = true
	}
	return cases, nil
}

func (c *Case) normalize() error {
	if strings.TrimSpace(c.Query) == "" || strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Artist) == "" {
		return fmt.Errorf("query, artist and title are required")
	}
	kind, err := domain.ParseResultKind(c.Kind)
	if err != nil || !slices.Contains(scoredKinds, kind) {
		return fmt.Errorf("kind %q must be track, album or artist", c.Kind)
	}
	c.kind = kind
	if c.Within < 0 {
		return fmt.Errorf("within %d is negative", c.Within)
	}
	if c.KnownFailing != "" && !knownFailingForm.MatchString(c.KnownFailing) {
		return fmt.Errorf("known_failing %q must be a ticket number like #123", c.KnownFailing)
	}
	if err := c.normalizeIdentity(); err != nil {
		return err
	}
	return c.checkLabels()
}

func (c *Case) normalizeIdentity() error {
	if c.kind == domain.ResultKindTrack {
		if c.MBID != "" || len(c.ISRCs) == 0 {
			return fmt.Errorf("a track is identified by isrcs alone")
		}
		for i, isrc := range c.ISRCs {
			c.ISRCs[i] = songfacts.CanonicalISRC(isrc)
			if !songfacts.IsCanonicalISRC(c.ISRCs[i]) {
				return fmt.Errorf("isrc %q is not an ISRC", isrc)
			}
		}
		return nil
	}
	if len(c.ISRCs) != 0 || !IsMBID(c.MBID) {
		return fmt.Errorf("an album or artist is identified by a lowercase mbid alone")
	}
	return nil
}

func (c Case) checkLabels() error {
	known := versiontag.KnownLabels()
	for _, label := range c.Labels {
		if !known[label] {
			return fmt.Errorf("label %q is not a versiontag label", label)
		}
	}
	if c.kind == domain.ResultKindArtist {
		if len(c.Labels) != 0 {
			return fmt.Errorf("an artist has no version labels")
		}
		return nil
	}
	if want := c.titleLabels(); !discoveryEval.LabelsAgree(want, c.Labels) {
		return fmt.Errorf("labels %v disagree with the labels %v of title %q", c.Labels, want, c.Title)
	}
	return nil
}

func (c Case) titleLabels() []string { return discoveryEval.ResultLabels(c.kind, c.Title) }

func (c Case) identity() string {
	if c.kind == domain.ResultKindTrack {
		return strings.Join(c.ISRCs, ",")
	}
	return c.MBID
}

func (c Case) Hits(r domain.SearchResult) bool {
	if r.Kind != c.kind {
		return false
	}
	switch c.kind {
	case domain.ResultKindTrack:
		return slices.Contains(c.ISRCs, songfacts.CanonicalISRC(r.ISRC)) && discoveryEval.LabelsAgree(discoveryEval.ResultLabels(r.Kind, r.Title), c.Labels)
	case domain.ResultKindAlbum:
		return strings.EqualFold(r.ReleaseGroupMBID, c.MBID) && discoveryEval.LabelsAgree(discoveryEval.ResultLabels(r.Kind, r.Title), c.Labels)
	case domain.ResultKindArtist:
		return strings.EqualFold(r.ArtistMBID, c.MBID)
	default:
		return false
	}
}

func (c Case) withinRank() int {
	if c.Within > 0 {
		return c.Within
	}
	return defaultWithin
}

type Verdict int

const (
	VerdictUnknown Verdict = iota
	Pass
	Fail
	StillFailing
	Fixed
	Errored
)

func (v Verdict) String() string {
	switch v {
	case Pass:
		return "pass"
	case Fail:
		return "fail"
	case StillFailing:
		return "known_failing"
	case Fixed:
		return "fixed_but_marked_known_failing"
	case Errored:
		return "error"
	default:
		return "unknown"
	}
}

func (v Verdict) isUnexpected() bool {
	return v == Fail || v == Fixed || v == Errored
}

type Result struct {
	Case     Case                         `json:"case"`
	Verdict  Verdict                      `json:"verdict"`
	Position int                          `json:"position"`
	Top      *discoveryEval.ResultSummary `json:"top,omitempty"`
	Error    string                       `json:"error,omitempty"`
}

func (v Verdict) MarshalJSON() ([]byte, error) { return json.Marshal(v.String()) }

type Report struct {
	Results []Result `json:"results"`
}

func Run(ctx context.Context, cases []Case, searcher discoveryEval.Searcher, concurrency int) Report {
	results := make([]Result, len(cases))
	group := fanout.New(concurrency)
	for i, c := range cases {
		group.Go(func() { results[i] = runCase(ctx, c, searcher) })
	}
	group.Wait()
	return Report{Results: results}
}

func runCase(ctx context.Context, c Case, searcher discoveryEval.Searcher) Result {
	res := Result{Case: c, Position: -1}
	shown, err := searcher.Search(ctx, c.Query)
	if err != nil {
		res.Verdict = Errored
		res.Error = err.Error()
		return res
	}
	if len(shown) > 0 {
		res.Top = &discoveryEval.ResultSummary{Kind: shown[0].Kind.String(), Title: shown[0].Title, Subtitle: shown[0].Subtitle}
	}
	res.Position = positionAmongKind(shown, c)
	res.Verdict = verdictFor(c, res.Position)
	return res
}

func positionAmongKind(shown []domain.SearchResult, c Case) int {
	rank := 0
	for _, r := range shown {
		if r.Kind != c.kind {
			continue
		}
		if c.Hits(r) {
			return rank
		}
		rank++
	}
	return -1
}

func verdictFor(c Case, position int) Verdict {
	hit := position >= 0 && position < c.withinRank()
	switch {
	case hit && c.KnownFailing != "":
		return Fixed
	case hit:
		return Pass
	case c.KnownFailing != "":
		return StillFailing
	default:
		return Fail
	}
}

func (r Report) count(match func(Result) bool) int {
	n := 0
	for _, res := range r.Results {
		if match(res) {
			n++
		}
	}
	return n
}

func (r Report) Hits() int {
	return r.count(func(g Result) bool { return g.Verdict == Pass || g.Verdict == Fixed })
}

func (r Report) Top1Hits() int {
	return r.count(func(g Result) bool {
		return g.Position == 0 && (g.Verdict == Pass || g.Verdict == Fixed)
	})
}

func (r Report) Unexpected() int {
	return r.count(func(g Result) bool { return g.Verdict.isUnexpected() })
}

func (r Report) KnownFailing() int {
	return r.count(func(g Result) bool { return g.Verdict == StillFailing })
}

func (r Report) Metrics() []discoveryEval.NamedMetric {
	return []discoveryEval.NamedMetric{
		{Name: "golden.hit_rate", Value: share(r.Hits(), len(r.Results)), HigherIsBetter: true},
		{Name: "golden.top1_rate", Value: share(r.Top1Hits(), len(r.Results)), HigherIsBetter: true},
		{Name: "golden.unexpected_cases", Value: float64(r.Unexpected()), HigherIsBetter: false},
	}
}

func (r Report) Failures() []discoveryEval.FailureRecord {
	out := []discoveryEval.FailureRecord{}
	for _, res := range r.Results {
		if res.Verdict == Pass {
			continue
		}
		attrs := discoveryEval.QueryAttrs(res.Case.Query)
		attrs["kind"] = res.Case.Kind
		reason := res.Verdict.String()
		if res.Case.KnownFailing != "" {
			reason += " " + res.Case.KnownFailing
		}
		out = append(out, discoveryEval.FailureRecord{Query: res.Case.Query, Reason: reason, Attrs: attrs})
	}
	return out
}

var _ discoveryEval.HarnessReport = Report{}

func share(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}
