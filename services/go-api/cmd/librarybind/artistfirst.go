package main

import (
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"slices"
)

const reasonArtistFirstPrefix = "artist-first: "

type replacedISRC struct {
	ISRC           string  `json:"isrc"`
	Refusal        string  `json:"refusal"`
	CatalogSeconds float64 `json:"catalog_seconds"`
	Independent    bool    `json:"independent"`
}

type chosenArtist struct {
	option artistOption
	found  bool
}

type artistFirst struct {
	sources []artistSource
	mb      isrcLookup
	held    heldIndex
	chosen  map[string]chosenArtist
}

func newArtistFirst(sources []artistSource, mb isrcLookup, held heldIndex) *artistFirst {
	return &artistFirst{sources: sources, mb: mb, held: held, chosen: map[string]chosenArtist{}}
}

func wantsArtistFirst(found identity) bool {
	return found.Status == statusUnbound && slices.Contains(artistFirstReasons, found.Reason)
}

var artistFirstReasons = []string{reasonNoISRC, reasonISRCNotFound, reasonRefusedPrefix + string(guardTitle), reasonRefusedPrefix + string(guardLength)}

func isRepairableRefusal(reason string) bool {
	return reason == reasonRefusedPrefix+string(guardTitle) || reason == reasonRefusedPrefix+string(guardLength)
}

func ignoringISRC(t libraryTrack, c candidate) judged {
	probe := t
	probe.ISRC = c.ISRC
	return judge(probe, c)
}

func isPlausible(t libraryTrack, c candidate) bool {
	return ignoringISRC(t, c).isGuarded()
}

func outcomeIgnoringISRC(t libraryTrack, from source, found []candidate, lookupErr error) sourceOutcome {
	identified := slices.DeleteFunc(slices.Clone(found), func(c candidate) bool { return canonicalISRC(c.ISRC) == "" })
	outcome := sourceOutcome{Source: from, Candidates: make([]judged, 0, len(identified))}
	if lookupErr != nil {
		outcome.Error = lookupErr.Error()
		outcome.Ambiguous = errors.Is(lookupErr, errTooManyMatches)
		return outcome
	}
	for _, c := range identified {
		outcome.Candidates = append(outcome.Candidates, ignoringISRC(t, c))
	}
	outcome.Winner, outcome.Ambiguous = pickWinner(t.FileSeconds, outcome.Candidates)
	if namesSeveralISRCs(outcome.Candidates) {
		outcome.Winner, outcome.Ambiguous = "", true
	}
	return outcome
}

func namesSeveralISRCs(all []judged) bool {
	var isrcs []string
	for _, j := range all {
		if isrc := canonicalISRC(j.ISRC); j.isGuarded() && !slices.Contains(isrcs, isrc) {
			isrcs = append(isrcs, isrc)
		}
	}
	return len(isrcs) > 1
}

var errTooManyMatches = errors.New("more plausible recordings than can be told apart")

func stopsTheRun(ctx context.Context, err error) bool {
	return errors.Is(err, errCatalogBlocked) || ctx.Err() != nil
}

func (a *artistFirst) refine(ctx context.Context, t libraryTrack, found identity) (identity, error) {
	keys := domain.CleanSearchKeys(t.Title, t.Artist, "")
	if !wantsArtistFirst(found) || keys.Artist == "" || keys.Title == "" {
		return found, nil
	}
	outcomes, err := a.searchSources(ctx, t, keys)
	if err != nil {
		return found, err
	}
	agreement, proof, err := a.confirm(ctx, t, outcomes)
	found.Evidence.ArtistFirst = proof
	if err != nil || agreement.ISRC == "" {
		return found, err
	}
	return found.boundByArtistFirst(t, agreement), nil
}

func (a *artistFirst) searchSources(ctx context.Context, t libraryTrack, keys domain.SearchKeys) ([]sourceOutcome, error) {
	outcomes := make([]sourceOutcome, 0, len(a.sources))
	for _, s := range a.sources {
		found, err := a.songsFrom(ctx, s, t, keys)
		if stopsTheRun(ctx, err) {
			return nil, errors.Join(err, ctx.Err())
		}
		outcomes = append(outcomes, outcomeIgnoringISRC(t, s.Name(), found, err))
	}
	return outcomes, nil
}

func (a *artistFirst) songsFrom(ctx context.Context, s artistSource, t libraryTrack, keys domain.SearchKeys) ([]candidate, error) {
	picked, err := a.artistFor(ctx, s, t, keys)
	if err != nil || !picked.found {
		return nil, err
	}
	return s.Songs(ctx, picked.option.providerArtist, picked.option.view, t, keys)
}

func (a *artistFirst) artistFor(ctx context.Context, s artistSource, t libraryTrack, keys domain.SearchKeys) (chosenArtist, error) {
	cacheKey := string(s.Name()) + "\x1f" + heldKey(t.UserID, t.Artist)
	if cached, ok := a.chosen[cacheKey]; ok {
		return cached, nil
	}
	options, err := a.optionsFor(ctx, s, keys.Artist, t.Artist)
	if err != nil {
		return chosenArtist{}, err
	}
	picked, found := chooseArtist(a.held.holdingsFor(t), t.Artist, options)
	a.chosen[cacheKey] = chosenArtist{option: picked, found: found}
	return a.chosen[cacheKey], nil
}

func (a *artistFirst) optionsFor(ctx context.Context, s artistSource, name, storedName string) ([]artistOption, error) {
	artists, err := s.FindArtists(ctx, name)
	if err != nil {
		return nil, err
	}
	options := make([]artistOption, 0, maxArtistOptions)
	for i, artist := range artists {
		if i >= maxArtistOptions && artistKey(artist.Name) != artistKey(storedName) {
			continue
		}
		view, err := s.Catalog(ctx, artist)
		if err != nil {
			return nil, err
		}
		options = append(options, artistOption{providerArtist: artist, view: view})
	}
	return options, nil
}

type agreement struct {
	ISRC           string
	Sources        []sourceOutcome
	CatalogSeconds float64
}

func (a *artistFirst) confirm(ctx context.Context, t libraryTrack, outcomes []sourceOutcome) (agreement, []sourceOutcome, error) {
	winners := confirmingOutcomes(outcomes)
	isrcs := distinctWinnerISRCs(winners)
	switch {
	case hasAmbiguousSource(outcomes) || len(isrcs) != 1:
		return agreement{}, outcomes, nil
	case len(winners) >= 2:
		return agreementOf(isrcs[0], winners), outcomes, nil
	}
	mbOutcome, err := a.musicBrainzCheck(ctx, t, isrcs[0])
	proof := append(slices.Clone(outcomes), mbOutcome)
	if err != nil || mbOutcome.Winner == "" || mbOutcome.Ambiguous {
		return agreement{}, proof, err
	}
	return agreementOf(isrcs[0], append(slices.Clone(winners), mbOutcome)), proof, nil
}

func (a *artistFirst) musicBrainzCheck(ctx context.Context, t libraryTrack, isrc string) (sourceOutcome, error) {
	found, err := a.mb.ByISRC(ctx, isrc)
	if stopsTheRun(ctx, err) {
		return sourceOutcome{}, errors.Join(err, ctx.Err())
	}
	return outcomeIgnoringISRC(t, a.mb.Name(), found, err), nil
}

func winningCandidate(o sourceOutcome) judged {
	for _, j := range o.Candidates {
		if j.ID == o.Winner {
			return j
		}
	}
	return judged{}
}

func distinctWinnerISRCs(winners []sourceOutcome) []string {
	var isrcs []string
	for _, o := range winners {
		if isrc := canonicalISRC(winningCandidate(o).ISRC); !slices.Contains(isrcs, isrc) {
			isrcs = append(isrcs, isrc)
		}
	}
	return isrcs
}

func (a agreement) isDeezerAndApple() bool {
	return slices.ContainsFunc(a.Sources, func(o sourceOutcome) bool { return o.Source == sourceDeezer }) &&
		slices.ContainsFunc(a.Sources, func(o sourceOutcome) bool { return o.Source == sourceApple })
}

func agreementOf(isrc string, sources []sourceOutcome) agreement {
	return agreement{ISRC: isrc, Sources: sources, CatalogSeconds: winningCandidate(sources[0]).LengthSeconds}
}

func (i identity) boundByArtistFirst(t libraryTrack, agreed agreement) identity {
	priorReason := i.Reason
	i.Status = statusBound
	i.ISRC = agreed.ISRC
	i.recordConfirmations(agreed.Sources)
	i.Reason = reasonArtistFirstPrefix + i.Reason
	if storedISRC := canonicalISRC(t.ISRC); storedISRC != "" && storedISRC != agreed.ISRC {
		i.Evidence.Replaced = &replacedISRC{ISRC: storedISRC, Refusal: priorReason, CatalogSeconds: agreed.CatalogSeconds, Independent: agreed.isDeezerAndApple()}
	}
	return i
}
