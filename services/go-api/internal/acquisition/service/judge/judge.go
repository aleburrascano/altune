package judge

import (
	"altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/shared/textnorm"
	"altune/go-api/internal/shared/versiontag"
	"math"
	"regexp"
	"slices"
	"strings"
)

type Kind string

const (
	ArtTrack    Kind = "art_track"
	PinnedVideo Kind = "pinned_video"
	Upload      Kind = "upload"
)

type Target struct {
	Title    string
	Artist   string
	Album    string
	Length   float64
	Explicit *bool
}

type Candidate struct {
	URL      string
	Kind     Kind
	Title    string
	Artists  []string
	Album    string
	Length   float64
	Explicit bool
}

type Outcome string

const (
	Accept Outcome = "accept"
	Verify Outcome = "verify"
	Reject Outcome = "reject"
)

type Decision struct {
	Outcome Outcome
	Reason  string
}

type ReplaceOutcome string

const (
	ReplaceSwap   ReplaceOutcome = "swap"
	ReplaceUnsure ReplaceOutcome = "unsure"
)

const (
	lengthAgreeSlackSecs = 3.0
	lengthAgreeFraction  = 0.02
)

var creditOnlyBrackets = regexp.MustCompile(`(?i)\s*[\(\[](?:feat\.?|ft\.?|featuring|with|bonus)\b[^\)\]]*[\)\]]`)

func Evaluate(t Target, c Candidate) Decision {
	if titleIdentity(c.Title) != titleIdentity(t.Title) {
		return Decision{Reject, "different title"}
	}
	if !domain.Credits(c.Artists, t.Artist) {
		return Decision{Reject, "artist not credited"}
	}
	if want, got := versiontag.Labels(t.Title), versiontag.Labels(c.Title); !slices.Equal(want, got) {
		return Decision{Reject, "different version: want " + labelList(want) + ", got " + labelList(got)}
	}
	if c.Kind == Upload {
		return Decision{Verify, "upload: needs a fingerprint match against the official track"}
	}
	switch {
	case t.Length > 0 && c.Length > 0 && !lengthsAgree(t.Length, c.Length):
		return Decision{Reject, "length disagrees with the catalog length"}
	case t.Length <= 0 && albumKnown(t) && !sameAlbum(t.Album, c.Album):
		return Decision{Reject, "no catalog length and another album: cannot confirm the cut"}
	default:
		return Decision{Accept, "official, same title, artist and version"}
	}
}

func Choose(t Target, candidates []Candidate) (Candidate, Decision, bool) {
	best, bestDecision, found := Candidate{}, Decision{}, false
	for _, c := range candidates {
		d := Evaluate(t, c)
		if d.Outcome != Accept {
			continue
		}
		if !found || preferred(t, c, best) {
			best, bestDecision, found = c, d, true
		}
	}
	return best, bestDecision, found
}

func Replace(t Target, currentLength float64, chosen Candidate) ReplaceOutcome {
	if t.Length > 0 || currentLength <= 0 || lengthsAgree(currentLength, chosen.Length) {
		return ReplaceSwap
	}
	return ReplaceUnsure
}

func preferred(t Target, a, b Candidate) bool {
	if ra, rb := kindRank(a.Kind), kindRank(b.Kind); ra != rb {
		return ra < rb
	}
	if ea, eb := exactTitle(t.Title, a.Title), exactTitle(t.Title, b.Title); ea != eb {
		return ea
	}
	if sa, sb := sameAlbum(t.Album, a.Album), sameAlbum(t.Album, b.Album); sa != sb {
		return sa
	}
	if a.Explicit != b.Explicit {
		return a.Explicit == (t.Explicit == nil || *t.Explicit)
	}
	if t.Length > 0 {
		return math.Abs(a.Length-t.Length) < math.Abs(b.Length-t.Length)
	}
	return false
}

func kindRank(k Kind) int {
	switch k {
	case PinnedVideo:
		return 0
	case ArtTrack:
		return 1
	default:
		return 2
	}
}

func lengthsAgree(want, got float64) bool {
	return math.Abs(want-got) <= math.Max(lengthAgreeSlackSecs, want*lengthAgreeFraction)
}

func titleIdentity(title string) string {
	tokens := versiontag.Tokens(creditOnlyBrackets.ReplaceAllString(versiontag.WithoutFeatureCredits(title), ""))
	return strings.Join(versiontag.WithoutVersionPhrases(tokens), " ")
}

func exactTitle(want, got string) bool {
	return textnorm.NormalizeForIdentity(want) == textnorm.NormalizeForIdentity(got)
}

func albumKnown(t Target) bool {
	album := textnorm.NormalizeForMatch(t.Album)
	return album != "" && album != textnorm.NormalizeForMatch(t.Title)
}

func sameAlbum(want, got string) bool {
	w := textnorm.NormalizeForMatch(want)
	return w != "" && w == textnorm.NormalizeForMatch(got)
}

func labelList(labels []string) string {
	if len(labels) == 0 {
		return "none"
	}
	return strings.Join(labels, "+")
}
