package audiojudge

import (
	"altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/songfacts"
	"altune/go-api/internal/shared/textnorm"
	"altune/go-api/internal/shared/versiontag"
	"fmt"
	"maps"
	"slices"
	"strings"
)

type Kind string

const (
	Proven       Kind = "proven"
	Likely       Kind = "likely"
	Unknown      Kind = "unknown"
	Contradicted Kind = "contradicted"
)

type IdentityVersion string

type Verdict struct {
	Kind              Kind
	Grade             grade.Grade
	LowersStoredGrade bool
	Reasons           []string
	Linked            []ports.LinkedRecording
	JudgedAgainst     IdentityVersion
}

func (v Verdict) HoldsFor(current IdentityVersion) bool {
	return v.JudgedAgainst == current
}

type Identity struct {
	Version        IdentityVersion
	ISRC           string
	MBID           string
	MergedMBIDs    []string
	Doubted        bool
	CatalogLengths map[string]float64
	Title          string
	Artist         string
}

func (i Identity) hasCatalogIdentity() bool {
	return i.MBID != "" || i.ISRC != "" || slices.ContainsFunc(slices.Collect(maps.Values(i.CatalogLengths)), func(length float64) bool { return length > 0 })
}

type Preview struct {
	BitErrorRate   float64
	OverlapSeconds float64
}

type Audio struct {
	AcoustID            []ports.AcoustIDResult
	Preview             *Preview
	Length              float64
	RecordingFacts      map[string]ports.RecordingFacts
	RequireVersionFacts bool
}

type Source struct {
	Kind grade.SourceKind
}

type Input struct {
	Identity       Identity
	Audio          Audio
	Source         Source
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
	linked      []ports.LinkedRecording
	doubted     bool
	acoustID    acoustIDStance
	preview     previewStance
	tagsDiffer  bool
	lengthFits  bool
	lengthOuter bool
	official    bool
	reasons     []string
}

func Judge(in Input, cuts Cuts) Verdict {
	if !in.Identity.hasCatalogIdentity() {
		return Verdict{
			Kind:          Unknown,
			Grade:         grade.BestAvailable,
			Reasons:       []string{"no catalog identity (no MBID, ISRC or catalog length) to judge against; this is the best available"},
			JudgedAgainst: in.Identity.Version,
		}
	}
	ev := gather(in, cuts)
	kind := ev.kind()
	return Verdict{
		Kind:              kind,
		Grade:             grade.Of(ev.fingerprint(kind), ev.provenance(), in.Source.Kind),
		LowersStoredGrade: kind == Contradicted || ev.demotedByLength(),
		Reasons:           ev.reasons,
		Linked:            ev.linked,
		JudgedAgainst:     in.Identity.Version,
	}
}

func gather(in Input, cuts Cuts) evidence {
	var ev evidence
	ev.official = in.Source.Kind.IsOfficial() || in.Source.Kind == grade.UserUpload
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

func (ev evidence) kind() Kind {
	switch {
	case ev.isProven():
		return Proven
	case !ev.isFor() && ev.againstCount() >= 2:
		return Contradicted
	case ev.isLikely():
		return Likely
	}
	return Unknown
}

func (ev evidence) fingerprint(kind Kind) grade.Fingerprint {
	switch {
	case kind == Contradicted:
		return grade.Contradicts
	case kind == Proven:
		return grade.Matches
	case kind == Likely:
		return grade.Approximates
	case ev.acoustID == acoustIDOther:
		return grade.Unsupported
	}
	return grade.Silent
}

func (ev evidence) provenance() shared.AcquisitionProvenance {
	if ev.lengthFits {
		return shared.ProvenanceCorroborated
	}
	return shared.ProvenanceBestEffort
}

func (ev *evidence) judgeAcoustID(in Input) {
	survivors := recordingsMatchingLength(in.Audio.Length, in.Audio.AcoustID)
	ev.linked = survivors
	if len(survivors) == 0 {
		ev.note("acoustid: no linked recording of the file's length")
		return
	}
	if i := slices.IndexFunc(survivors, func(r ports.LinkedRecording) bool { return in.Audio.isBound(in.Identity, r) }); i >= 0 {
		ev.acoustID = acoustIDBound
		ev.note("acoustid: links the bound recording %s", survivors[i].MBID)
		return
	}
	sameTitle := sameTitleLinks(in.Identity, survivors)
	if i := slices.IndexFunc(sameTitle, func(r ports.LinkedRecording) bool { return in.Audio.isDuplicate(in.Identity, r) }); i >= 0 && !mostTitlesDiffer(in.Identity, survivors) {
		ev.acoustID = acoustIDDuplicate
		ev.note("acoustid: links %s, a same-title duplicate of the bound recording", sameTitle[i].MBID)
		return
	}
	if unverified := slices.IndexFunc(sameTitle, func(r ports.LinkedRecording) bool { return in.Audio.cannotVerifyVersionOf(r) }); unverified >= 0 {
		ev.note("acoustid: links %s, a same-title recording whose version could not be verified", sameTitle[unverified].MBID)
		return
	}
	named := slices.IndexFunc(survivors, func(r ports.LinkedRecording) bool { return textnorm.NormalizeForMatch(r.Title) != "" })
	if named < 0 {
		ev.note("acoustid: links only untitled recordings, which name nothing")
		return
	}
	ev.acoustID = acoustIDOther
	ev.note("acoustid: names another recording %q (%s)", survivors[named].Title, survivors[named].MBID)
}

func (a Audio) isBound(identity Identity, recording ports.LinkedRecording) bool {
	if isBoundByID(identity, recording) {
		return true
	}
	facts, found := a.RecordingFacts[recording.MBID]
	return found && identity.ISRC != "" && slices.ContainsFunc(facts.ISRCs, func(isrc string) bool {
		return songfacts.CanonicalISRC(isrc) == songfacts.CanonicalISRC(identity.ISRC)
	})
}

func VersionLookupCandidates(in Input) []string {
	var candidates []string
	survivors := recordingsMatchingLength(in.Audio.Length, in.Audio.AcoustID)
	for _, recording := range slices.Concat(sameTitleLinks(in.Identity, survivors), survivors) {
		if !isBoundByID(in.Identity, recording) && !slices.Contains(candidates, recording.MBID) {
			candidates = append(candidates, recording.MBID)
		}
	}
	return candidates
}

func isBoundByID(identity Identity, recording ports.LinkedRecording) bool {
	return recording.MBID == identity.MBID || slices.Contains(identity.MergedMBIDs, recording.MBID)
}

func recordingsMatchingLength(audioLength float64, results []ports.AcoustIDResult) []ports.LinkedRecording {
	var survivors []ports.LinkedRecording
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
	isLengthUnknown := linkLength == 0 || audioLength <= 0
	return isLengthUnknown || domain.LengthAuthoritative.Agrees(audioLength, linkLength)
}

func sameTitleLinks(identity Identity, survivors []ports.LinkedRecording) []ports.LinkedRecording {
	return slices.DeleteFunc(slices.Clone(survivors), func(recording ports.LinkedRecording) bool {
		return textnorm.NormalizeForMatch(recording.Title) != textnorm.NormalizeForMatch(identity.Title) ||
			!sharesArtist(identity.Artist, recording.Artists)
	})
}

func (a Audio) cannotVerifyVersionOf(recording ports.LinkedRecording) bool {
	_, found := a.RecordingFacts[recording.MBID]
	return a.RequireVersionFacts && !found
}

func (a Audio) isDuplicate(identity Identity, recording ports.LinkedRecording) bool {
	facts, found := a.RecordingFacts[recording.MBID]
	isOtherRecordingByISRC := identity.ISRC != "" && (!found || len(facts.ISRCs) != 0)
	if a.cannotVerifyVersionOf(recording) || isOtherRecordingByISRC {
		return false
	}
	title := recording.Title
	if found {
		title = titleWithDisambiguation(facts)
	}
	return versiontag.ParseTags(title).Equal(versiontag.ParseTags(identity.Title))
}

func titleWithDisambiguation(facts ports.RecordingFacts) string {
	if facts.Disambiguation == "" {
		return facts.Title
	}
	return facts.Title + " (" + facts.Disambiguation + ")"
}

func mostTitlesDiffer(identity Identity, survivors []ports.LinkedRecording) bool {
	titles := make(map[string]bool)
	for _, recording := range survivors {
		if core := textnorm.NormalizeForMatch(recording.Title); core != "" {
			titles[core] = true
		}
	}
	differing := len(titles)
	if titles[textnorm.NormalizeForMatch(identity.Title)] {
		differing--
	}
	return differing*2 > len(titles)
}

func sharesArtist(artist string, credits []string) bool {
	wanted := textnorm.NormalizeForMatch(artist)
	return wanted != "" && slices.ContainsFunc(credits, func(credit string) bool {
		credited := textnorm.NormalizeForMatch(credit)
		return credited != "" && (strings.Contains(wanted, credited) || strings.Contains(credited, wanted))
	})
}

func (ev *evidence) judgePreview(preview *Preview, cuts Cuts) {
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

func (ev *evidence) judgeTags(in Input) {
	if in.CandidateTitle == "" {
		return
	}
	identity, candidate := versiontag.ParseTags(in.Identity.Title), versiontag.ParseTags(in.CandidateTitle)
	ev.tagsDiffer = !identity.Equal(candidate)
	if ev.tagsDiffer {
		ev.note("version tags differ: bound %+v, candidate %+v", identity, candidate)
	}
}

func (ev *evidence) judgeLength(in Input) {
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
	ev.lengthFits = slices.ContainsFunc(known, func(catalog float64) bool { return domain.LengthAuthoritative.Agrees(catalog, measured) })
	ev.lengthOuter = !ev.lengthFits
	if ev.lengthOuter {
		ev.note("length %.0fs is outside every catalog length %v", measured, known)
	}
}
