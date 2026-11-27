package candidateranking

import (
	"altune/go-api/internal/download/internal/core/ports"
	"fmt"
	"math"
	"strings"
)

type Track struct {
	ID              string
	Title           string
	Artist          string
	ExpectedLength  float64
	CatalogExplicit *bool
}

type Stage string

const (
	StageIdentity  Stage = "identity"
	StageQualifier Stage = "qualifier"
	StageDRM       Stage = ports.UnplayableDRM
	StagePreview   Stage = ports.UnplayablePreview
)

type Refusal struct {
	Candidate ports.AudioCandidate
	Stage     Stage
	Reason    string
}

type Evaluation struct {
	Candidate         ports.AudioCandidate
	IdentityScore     float64
	MetadataRank      float64
	QualifierDistance int
	IsTopic           bool
	ArtistMatch       bool
	FeatureMatch      bool
}

const (
	identityScoreScale = 10
	metadataRankScale  = 1000
)

func evaluationOf(e entry) Evaluation {
	return Evaluation{
		Candidate:         e.candidate,
		IdentityScore:     math.Round(e.ident*identityScoreScale) / identityScoreScale,
		MetadataRank:      math.Round(e.meta*metadataRankScale) / metadataRankScale,
		QualifierDistance: e.qualDistance,
		IsTopic:           e.uploader == ChannelTopic,
		ArtistMatch:       e.artistMatch,
		FeatureMatch:      e.featMatch,
	}
}

func Rank(track Track, candidates []ports.AudioCandidate) (ranked []ports.AudioCandidate, refused []Refusal, evaluated []Evaluation) {
	maxViews := maxViewCount(candidates)
	var b buckets
	for _, c := range candidates {
		if refusal, kept := b.place(track, c, maxViews); !kept {
			refused = append(refused, refusal)
		}
	}
	return b.ranked(), refused, b.evaluated
}

type buckets struct {
	resolved, topic, other []entry
	evaluated              []Evaluation
}

func (b *buckets) place(track Track, c ports.AudioCandidate, maxViews int64) (Refusal, bool) {
	if c.Unplayable != "" {
		return Refusal{Candidate: c, Stage: unplayableStage(c.Unplayable), Reason: "soundcloud " + c.Unplayable}, false
	}
	e := score(track, c, maxViews)
	b.evaluated = append(b.evaluated, evaluationOf(e))
	if carriesUnrequestedRerecording(track.Title, c.Title) {
		return qualifierRefusal(c, []string{"re-recorded"}), false
	}
	if c.IsResolved() {
		b.resolved = append(b.resolved, e)
		return Refusal{}, true
	}
	return b.placeUnresolved(track, e)
}

func (b *buckets) placeUnresolved(track Track, e entry) (Refusal, bool) {
	if e.ident < identityMin && !e.artistMatch {
		return identityRefusal(e), false
	}
	veto, qualifiers := UnrequestedQualifiers(track.Title, track.Artist, e.candidate.Title)
	if len(veto) > 0 {
		return qualifierRefusal(e.candidate, veto), false
	}
	e.isOtherVersion = len(qualifiers) > 0
	switch e.uploader {
	case ChannelTopic:
		b.topic = append(b.topic, e)
	default:
		b.other = append(b.other, e)
	}
	return Refusal{}, true
}

func (b *buckets) ranked() []ports.AudioCandidate {
	var ranked []ports.AudioCandidate
	ranked = appendSorted(ranked, b.resolved, compareResolved)
	ranked = appendSorted(ranked, b.topic, compareTopic)
	return appendSorted(ranked, b.other, compareOther)
}

func unplayableStage(reason string) Stage {
	if reason == ports.UnplayablePreview {
		return StagePreview
	}
	return StageDRM
}

func qualifierRefusal(c ports.AudioCandidate, veto []string) Refusal {
	return Refusal{Candidate: c, Stage: StageQualifier, Reason: "unrequested " + strings.Join(veto, ", ")}
}

func identityRefusal(e entry) Refusal {
	return Refusal{
		Candidate: e.candidate,
		Stage:     StageIdentity,
		Reason:    fmt.Sprintf("identity %.0f below threshold %.0f", e.ident, identityMin),
	}
}
