package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/music"
	"fmt"
	"maps"
	"slices"
	"strings"
)

type JudgedKind string

const (
	JudgedProven       JudgedKind = "proven"
	JudgedLikely       JudgedKind = "likely"
	JudgedUnknown      JudgedKind = "unknown"
	JudgedContradicted JudgedKind = "contradicted"
)

type IdentityVersion string

type Verdict struct {
	Kind              JudgedKind
	Grade             domain.Grade
	LowersStoredGrade bool
	Reasons           []string
	Linked            []domain.LinkedRecording
	JudgedAgainst     IdentityVersion
}

func (v Verdict) HoldsFor(current IdentityVersion) bool {
	return v.JudgedAgainst == current
}

type IdentityEvidence struct {
	Version        IdentityVersion
	ISRC           string
	MBID           string
	MergedMBIDs    []string
	Doubted        bool
	CatalogLengths map[string]float64
	Title          string
	Artist         string
}

func (i IdentityEvidence) hasCatalogIdentity() bool {
	return i.MBID != "" || i.ISRC != "" || slices.ContainsFunc(slices.Collect(maps.Values(i.CatalogLengths)), func(length float64) bool { return length > 0 })
}

type PreviewEvidence struct {
	BitErrorRate   float64
	OverlapSeconds float64
}

type AudioEvidence struct {
	AcoustID            []domain.AcoustIDResult
	Preview             *PreviewEvidence
	Length              float64
	RecordingFacts      map[string]domain.RecordingFacts
	RequireVersionFacts bool
}

type SourceEvidence struct {
	Kind domain.SourceKind
}

type VerdictInput struct {
	Identity       IdentityEvidence
	Audio          AudioEvidence
	Source         SourceEvidence
	CandidateTitle string
}

type acoustIDStance int

const (
	acoustIDSilent acoustIDStance = iota
	acoustIDBound
	acoustIDDuplicate
	acoustIDOther
)

type previewStance int

const (
	previewSilent previewStance = iota
	previewProven
	previewLikely
	previewAbsent
)

type evidence struct {
	linked      []domain.LinkedRecording
	doubted     bool
	acoustID    acoustIDStance
	preview     previewStance
	tagsDiffer  bool
	lengthFits  bool
	lengthOuter bool
	official    bool
	reasons     []string
}

func Judge(in VerdictInput, cuts Cuts) Verdict {
	if !in.Identity.hasCatalogIdentity() {
		return Verdict{
			Kind:          JudgedUnknown,
			Grade:         domain.BestAvailable,
			Reasons:       []string{"no catalog identity (no MBID, ISRC or catalog length) to judge against; this is the best available"},
			JudgedAgainst: in.Identity.Version,
		}
	}
	ev := gather(in, cuts)
	kind := ev.kind()
	return Verdict{
		Kind:              kind,
		Grade:             domain.GradeOf(ev.fingerprint(kind), ev.provenance(), in.Source.Kind),
		LowersStoredGrade: kind == JudgedContradicted || ev.demotedByLength(),
		Reasons:           ev.reasons,
		Linked:            ev.linked,
		JudgedAgainst:     in.Identity.Version,
	}
}

func gather(in VerdictInput, cuts Cuts) evidence {
	var ev evidence
	ev.official = in.Source.Kind.IsOfficial() || in.Source.Kind == domain.UserUpload
	ev.doubted = in.Identity.Doubted
	ev.judgeAcoustID(in)
	ev.judgePreview(in.Audio.Preview, cuts)
	ev.judgeTags(in)
	ev.judgeLength(in)
	return ev
}

func (ev *evidence) note(format string, args ...any) {
	ev.reasons = append(ev.reasons, fmt.Sprintf(format, args...))
}

func (ev evidence) isFor() bool {
	return ev.acoustID == acoustIDBound || ev.preview == previewProven || ev.preview == previewLikely
}

func (ev evidence) againstCount() int {
	count := 0
	for _, against := range []bool{ev.acoustID == acoustIDOther, ev.preview == previewAbsent, ev.tagsDiffer, ev.lengthOuter} {
		if against {
			count++
		}
	}
	return count
}

func (ev evidence) isProven() bool {
	if ev.lengthOuter || ev.doubted {
		return false
	}
	return ev.acoustID == acoustIDBound || (ev.preview == previewProven && ev.official && !ev.tagsDiffer)
}

func (ev evidence) demotedByLength() bool {
	return ev.lengthOuter && !ev.doubted && (ev.acoustID == acoustIDBound || (ev.preview == previewProven && ev.official && !ev.tagsDiffer))
}

func (ev evidence) isLikely() bool {
	isUncontradictedDuplicate := ev.acoustID == acoustIDDuplicate && ev.preview != previewAbsent && !ev.lengthOuter
	return ev.acoustID == acoustIDBound || ev.preview == previewProven || ev.preview == previewLikely || isUncontradictedDuplicate
}

func (ev evidence) kind() JudgedKind {
	switch {
	case ev.isProven():
		return JudgedProven
	case !ev.isFor() && ev.againstCount() >= 2:
		return JudgedContradicted
	case ev.isLikely():
		return JudgedLikely
	}
	return JudgedUnknown
}

func (ev evidence) fingerprint(kind JudgedKind) domain.FingerprintResult {
	switch {
	case kind == JudgedContradicted:
		return domain.FingerprintContradicts
	case kind == JudgedProven:
		return domain.FingerprintMatches
	case kind == JudgedLikely:
		return domain.FingerprintApproximates
	case ev.acoustID == acoustIDOther:
		return domain.FingerprintUnsupported
	}
	return domain.FingerprintSilent
}

func (ev evidence) provenance() music.AcquisitionProvenance {
	if ev.lengthFits {
		return music.ProvenanceCorroborated
	}
	return music.ProvenanceBestEffort
}

func (ev *evidence) judgeAcoustID(in VerdictInput) {
	survivors := recordingsMatchingLength(in.Audio.Length, in.Audio.AcoustID)
	ev.linked = survivors
	if len(survivors) == 0 {
		ev.note("acoustid: no linked recording of the file's length")
		return
	}
	if i := slices.IndexFunc(survivors, func(r domain.LinkedRecording) bool { return in.Audio.isBound(in.Identity, r) }); i >= 0 {
		ev.acoustID = acoustIDBound
		ev.note("acoustid: links the bound recording %s", survivors[i].MBID)
		return
	}
	sameTitle := sameTitleLinks(in.Identity, survivors)
	if i := slices.IndexFunc(sameTitle, func(r domain.LinkedRecording) bool { return in.Audio.isDuplicate(in.Identity, r) }); i >= 0 && !mostTitlesDiffer(in.Identity, survivors) {
		ev.acoustID = acoustIDDuplicate
		ev.note("acoustid: links %s, a same-title duplicate of the bound recording", sameTitle[i].MBID)
		return
	}
	if unverified := slices.IndexFunc(sameTitle, func(r domain.LinkedRecording) bool { return in.Audio.cannotVerifyVersionOf(r) }); unverified >= 0 {
		ev.note("acoustid: links %s, a same-title recording whose version could not be verified", sameTitle[unverified].MBID)
		return
	}
	named := slices.IndexFunc(survivors, func(r domain.LinkedRecording) bool { return music.NormalizeForMatch(r.Title) != "" })
	if named < 0 {
		ev.note("acoustid: links only untitled recordings, which name nothing")
		return
	}
	ev.acoustID = acoustIDOther
	ev.note("acoustid: names another recording %q (%s)", survivors[named].Title, survivors[named].MBID)
}

func (a AudioEvidence) isBound(identity IdentityEvidence, recording domain.LinkedRecording) bool {
	if isBoundByID(identity, recording) {
		return true
	}
	facts, found := a.RecordingFacts[recording.MBID]
	return found && identity.ISRC != "" && slices.ContainsFunc(facts.ISRCs, func(isrc string) bool {
		return music.CanonicalISRC(isrc) == music.CanonicalISRC(identity.ISRC)
	})
}

func VersionLookupCandidates(in VerdictInput) []string {
	var candidates []string
	survivors := recordingsMatchingLength(in.Audio.Length, in.Audio.AcoustID)
	for _, recording := range slices.Concat(sameTitleLinks(in.Identity, survivors), survivors) {
		if !isBoundByID(in.Identity, recording) && !slices.Contains(candidates, recording.MBID) {
			candidates = append(candidates, recording.MBID)
		}
	}
	return candidates
}

func isBoundByID(identity IdentityEvidence, recording domain.LinkedRecording) bool {
	return recording.MBID == identity.MBID || slices.Contains(identity.MergedMBIDs, recording.MBID)
}

func recordingsMatchingLength(audioLength float64, results []domain.AcoustIDResult) []domain.LinkedRecording {
	var survivors []domain.LinkedRecording
	for _, result := range results {
		for _, recording := range result.Recordings {
			if recording.MBID != "" && linkLengthAgrees(audioLength, recording.Duration) {
				survivors = append(survivors, recording)
			}
		}
	}
	return survivors
}

func linkLengthAgrees(audioLength, linkLength float64) bool {
	isLengthUnknown := linkLength <= 0 || audioLength <= 0
	return isLengthUnknown || SameRecording(audioLength, linkLength)
}

func sameTitleLinks(identity IdentityEvidence, survivors []domain.LinkedRecording) []domain.LinkedRecording {
	return slices.DeleteFunc(slices.Clone(survivors), func(recording domain.LinkedRecording) bool {
		return music.NormalizeForMatch(recording.Title) != music.NormalizeForMatch(identity.Title) ||
			!sharesArtist(identity.Artist, recording.Artists)
	})
}

func (a AudioEvidence) cannotVerifyVersionOf(recording domain.LinkedRecording) bool {
	_, found := a.RecordingFacts[recording.MBID]
	return a.RequireVersionFacts && !found
}

func (a AudioEvidence) isDuplicate(identity IdentityEvidence, recording domain.LinkedRecording) bool {
	facts, found := a.RecordingFacts[recording.MBID]
	isOtherRecordingByISRC := identity.ISRC != "" && (!found || len(facts.ISRCs) != 0)
	if a.cannotVerifyVersionOf(recording) || isOtherRecordingByISRC {
		return false
	}
	title := recording.Title
	if found {
		title = titleWithDisambiguation(facts)
	}
	return music.ParseTags(title).Equal(music.ParseTags(identity.Title))
}

func titleWithDisambiguation(facts domain.RecordingFacts) string {
	if facts.Disambiguation == "" {
		return facts.Title
	}
	return facts.Title + " (" + facts.Disambiguation + ")"
}

func mostTitlesDiffer(identity IdentityEvidence, survivors []domain.LinkedRecording) bool {
	titles := make(map[string]bool)
	for _, recording := range survivors {
		if core := music.NormalizeForMatch(recording.Title); core != "" {
			titles[core] = true
		}
	}
	differing := len(titles)
	if titles[music.NormalizeForMatch(identity.Title)] {
		differing--
	}
	return differing*2 > len(titles)
}

func sharesArtist(artist string, credits []string) bool {
	wanted := music.NormalizeForMatch(artist)
	return wanted != "" && slices.ContainsFunc(credits, func(credit string) bool {
		credited := music.NormalizeForMatch(credit)
		return credited != "" && (strings.Contains(wanted, credited) || strings.Contains(credited, wanted))
	})
}

func (ev *evidence) judgePreview(preview *PreviewEvidence, cuts Cuts) {
	if preview == nil {
		ev.note("preview: none to compare")
		return
	}
	if preview.OverlapSeconds < cuts.MinOverlapSecs {
		ev.note("preview: overlap %.0fs is under the %.0fs needed", preview.OverlapSeconds, cuts.MinOverlapSecs)
		return
	}
	rate := preview.BitErrorRate
	switch {
	case rate <= cuts.ProvenMax:
		ev.preview = previewProven
	case rate <= cuts.LikelyMax:
		ev.preview = previewLikely
	case rate >= cuts.AbsentMin:
		ev.preview = previewAbsent
	}
	ev.note("preview: bit error rate %.2f", rate)
}

func (ev *evidence) judgeTags(in VerdictInput) {
	if in.CandidateTitle == "" {
		return
	}
	identity, candidate := music.ParseTags(in.Identity.Title), music.ParseTags(in.CandidateTitle)
	ev.tagsDiffer = !identity.Equal(candidate)
	if ev.tagsDiffer {
		ev.note("version tags differ: bound %+v, candidate %+v", identity, candidate)
	}
}

func (ev *evidence) judgeLength(in VerdictInput) {
	measured := in.Audio.Length
	var known []float64
	for _, catalog := range in.Identity.CatalogLengths {
		if catalog > 0 {
			known = append(known, catalog)
		}
	}
	if measured <= 0 || len(known) == 0 {
		return
	}
	ev.lengthFits = slices.ContainsFunc(known, func(catalog float64) bool { return SameRecording(catalog, measured) })
	ev.lengthOuter = !ev.lengthFits
	if ev.lengthOuter {
		ev.note("length %.0fs is outside every catalog length %v", measured, known)
	}
}
