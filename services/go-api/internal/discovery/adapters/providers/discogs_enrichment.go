package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

// AIDEV-NOTE: Discogs album enrichment (docs/providers/discogs.md caps 3–6).
// The structured-search resolve + master/release lookup the detail-open
// DiscogsEnrichmentService drives. Endpoint shapes live-probed 2026-06-22
// (docs/providers/discogs.md §4). Off the ranking path — display-only.
//
// AIDEV-DECISION: matching policy. Discogs has no ISRC/MBID, so an album is
// resolved by the structured `artist=+release_title=&type=master` search, then
// the top candidate whose combined "Artist - Title" contains BOTH the normalized
// album and (when present) the normalized artist is taken. This is intentionally
// fuzzy (a deluxe/reissue master can win over the original) and accepted for a
// display-only surface; it never touches ranking. Seed disambiguation with the
// MB-bridge discogs artist id once a future increment threads it through.

var _ ports.DiscogsEnricher = (*DiscogsAdapter)(nil)

const discogsCreditsCap = 60

// ResolveMasterID maps (artist, album) to a Discogs master id via the structured
// search, returning 0 when nothing matches (the service treats 0 as "nothing to
// enrich"). The match is the first master candidate whose combined title contains
// the normalized album title and — when artist is non-empty — the artist too.
func (a *DiscogsAdapter) ResolveMasterID(ctx context.Context, artist, album string) (int, error) {
	albumNorm := textnorm.NormalizeForMatch(album)
	if albumNorm == "" {
		return 0, nil
	}
	artistNorm := textnorm.NormalizeForMatch(artist)

	u := fmt.Sprintf(
		"https://api.discogs.com/database/search?artist=%s&release_title=%s&type=master&per_page=5",
		url.QueryEscape(artist), url.QueryEscape(album),
	)
	body, err := a.doGet(ctx, u)
	if err != nil {
		return 0, err
	}
	var resp discogsSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, err
	}

	for _, r := range resp.Results {
		titleNorm := textnorm.NormalizeForMatch(r.Title)
		if !strings.Contains(titleNorm, albumNorm) {
			continue
		}
		if artistNorm != "" && !strings.Contains(titleNorm, artistNorm) {
			continue
		}
		if r.ID > 0 {
			return r.ID, nil // a type=master result's id is the master id
		}
	}
	return 0, nil
}

// LookupAlbum fetches the master (genres/styles/year + per-track credits) and its
// main release (label/catalog/formats/companies/community + release-level
// credits), assembling them into a DiscogsEnrichment. A release fetch failure
// degrades gracefully to the master-only fields. A non-200 on the master returns
// an error so the service can degrade to empty.
func (a *DiscogsAdapter) LookupAlbum(ctx context.Context, masterID int) (domain.DiscogsEnrichment, error) {
	if masterID <= 0 {
		return domain.EmptyDiscogsEnrichment(), nil
	}

	master, err := a.fetchMaster(ctx, masterID)
	if err != nil {
		return domain.EmptyDiscogsEnrichment(), err
	}

	e := domain.EmptyDiscogsEnrichment()
	e.MasterID = masterID
	e.Genres = dedupeStrings(master.Genres)
	e.Styles = dedupeStrings(master.Styles)
	e.Year = master.Year
	trackCredits := collectCredits(flattenTrackCredits(master.Tracklist))

	if master.MainRelease > 0 {
		if rel, relErr := a.fetchRelease(ctx, master.MainRelease); relErr == nil {
			e.Country = rel.Country
			e.Labels = mapLabels(rel.Labels)
			e.Formats = mapFormats(rel.Formats)
			e.Companies = mapCompanies(rel.Companies)
			e.Community = mapCommunity(rel.Community)
			// Release-level extraartists is the curated album-wide credit list;
			// prefer it, falling back to the per-track set when it is empty.
			if releaseCredits := collectCredits(rel.ExtraArtists); len(releaseCredits) > 0 {
				e.Credits = releaseCredits
			} else {
				e.Credits = trackCredits
			}
		} else {
			e.Credits = trackCredits
		}
	} else {
		e.Credits = trackCredits
	}

	return e, nil
}

func (a *DiscogsAdapter) fetchMaster(ctx context.Context, masterID int) (*discogsMaster, error) {
	body, err := a.doGet(ctx, fmt.Sprintf("https://api.discogs.com/masters/%d", masterID))
	if err != nil {
		return nil, err
	}
	var m discogsMaster
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (a *DiscogsAdapter) fetchRelease(ctx context.Context, releaseID int) (*discogsReleaseDetail, error) {
	body, err := a.doGet(ctx, fmt.Sprintf("https://api.discogs.com/releases/%d", releaseID))
	if err != nil {
		return nil, err
	}
	var r discogsReleaseDetail
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// flattenTrackCredits concatenates every track's extraartists into one slice for
// deduping — the fallback when a release carries no album-wide credit list.
func flattenTrackCredits(tracks []discogsTrack) []discogsExtraArtist {
	out := make([]discogsExtraArtist, 0, len(tracks))
	for _, t := range tracks {
		out = append(out, t.ExtraArtists...)
	}
	return out
}

// collectCredits dedupes raw extraartists by (name, role) and caps the result so
// a heavily-credited release does not bloat the payload. Order is preserved
// (first occurrence wins) so the most prominent credits stay first.
func collectCredits(raw []discogsExtraArtist) []domain.DiscogsCredit {
	out := make([]domain.DiscogsCredit, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, c := range raw {
		name := strings.TrimSpace(c.Name)
		role := strings.TrimSpace(c.Role)
		if name == "" || role == "" {
			continue
		}
		key := name + "\x00" + role
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, domain.DiscogsCredit{Name: name, Role: role})
		if len(out) >= discogsCreditsCap {
			break
		}
	}
	return out
}

func mapLabels(labels []discogsLabelRef) []domain.DiscogsLabelRef {
	out := make([]domain.DiscogsLabelRef, 0, len(labels))
	seen := make(map[string]bool, len(labels))
	for _, l := range labels {
		name := strings.TrimSpace(l.Name)
		if name == "" {
			continue
		}
		key := name + "\x00" + l.Catno
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, domain.DiscogsLabelRef{Name: name, Catno: strings.TrimSpace(l.Catno)})
	}
	return out
}

func mapFormats(formats []discogsFormat) []string {
	out := make([]string, 0, len(formats))
	for _, f := range formats {
		parts := make([]string, 0, 1+len(f.Descriptions))
		if name := strings.TrimSpace(f.Name); name != "" {
			parts = append(parts, name)
		}
		for _, d := range f.Descriptions {
			if d = strings.TrimSpace(d); d != "" {
				parts = append(parts, d)
			}
		}
		if len(parts) > 0 {
			out = append(out, strings.Join(parts, " · "))
		}
	}
	return out
}

func mapCompanies(companies []discogsCompany) []domain.DiscogsCompany {
	out := make([]domain.DiscogsCompany, 0, len(companies))
	seen := make(map[string]bool, len(companies))
	for _, c := range companies {
		name := strings.TrimSpace(c.Name)
		role := strings.TrimSpace(c.EntityTypeName)
		if name == "" || role == "" {
			continue
		}
		key := name + "\x00" + role
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, domain.DiscogsCompany{Name: name, Role: role})
	}
	return out
}

func mapCommunity(c discogsCommunity) domain.DiscogsCommunity {
	return domain.DiscogsCommunity{
		Have:   c.Have,
		Want:   c.Want,
		Rating: c.Rating.Average,
		Votes:  c.Rating.Count,
	}
}

// dedupeStrings trims, drops empties, and removes duplicates while preserving
// order — Discogs genres/styles arrive ordered by relevance and we keep that.
func dedupeStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// ResolveArtistID maps an artist name to a Discogs artist id via the artist
// search, preferring the candidate whose normalized title equals the query name
// (else the top relevance result). Returns 0 when nothing matches.
func (a *DiscogsAdapter) ResolveArtistID(ctx context.Context, name string) (int, error) {
	nameNorm := textnorm.NormalizeForMatch(name)
	if nameNorm == "" {
		return 0, nil
	}
	artists, err := a.searchArtists(ctx, name)
	if err != nil {
		return 0, err
	}
	if len(artists) == 0 {
		return 0, nil
	}
	for _, art := range artists {
		if textnorm.NormalizeForMatch(art.Title) == nameNorm {
			return art.ID, nil
		}
	}
	return artists[0].ID, nil
}

// LookupArtist fetches an artist's bio, name history, group/member links, and
// external urls. A non-200 returns an error so the service can degrade to empty.
func (a *DiscogsAdapter) LookupArtist(ctx context.Context, artistID int) (domain.DiscogsArtistEnrichment, error) {
	if artistID <= 0 {
		return domain.EmptyDiscogsArtistEnrichment(), nil
	}

	body, err := a.doGet(ctx, fmt.Sprintf("https://api.discogs.com/artists/%d", artistID))
	if err != nil {
		return domain.EmptyDiscogsArtistEnrichment(), err
	}
	var full discogsArtistFull
	if err := json.Unmarshal(body, &full); err != nil {
		return domain.EmptyDiscogsArtistEnrichment(), err
	}

	e := domain.EmptyDiscogsArtistEnrichment()
	e.ArtistID = artistID
	e.Profile = cleanDiscogsProfile(full.Profile)
	e.RealName = strings.TrimSpace(full.RealName)
	e.Aliases = artistRefNames(full.Aliases)
	e.NameVariations = dedupeStrings(full.NameVariations)
	e.Members = artistRefNames(full.Members)
	e.Groups = artistRefNames(full.Groups)
	e.Links = mapLinks(full.URLs)
	return e, nil
}

// artistRefNames extracts the trimmed names from a list of artist references,
// dropping empties and duplicates while preserving order.
func artistRefNames(refs []discogsArtistRef) []string {
	out := make([]string, 0, len(refs))
	seen := make(map[string]bool, len(refs))
	for _, r := range refs {
		name := strings.TrimSpace(r.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

const discogsLinksCap = 10

// discogsLinkHosts maps a host substring to a human label for known providers.
var discogsLinkHosts = []struct{ host, label string }{
	{"wikipedia.org", "Wikipedia"},
	{"instagram.com", "Instagram"},
	{"twitter.com", "Twitter"},
	{"x.com", "Twitter"},
	{"facebook.com", "Facebook"},
	{"youtube.com", "YouTube"},
	{"soundcloud.com", "SoundCloud"},
	{"bandcamp.com", "Bandcamp"},
	{"genius.com", "Genius"},
	{"imdb.com", "IMDb"},
	{"open.spotify.com", "Spotify"},
	{"tiktok.com", "TikTok"},
}

// mapLinks turns raw urls into labeled links, deduped by URL and capped. The
// label is the known-provider name, else the bare host (www. stripped).
func mapLinks(urls []string) []domain.DiscogsLink {
	out := make([]domain.DiscogsLink, 0, len(urls))
	seen := make(map[string]bool, len(urls))
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			continue
		}
		seen[raw] = true
		out = append(out, domain.DiscogsLink{Label: linkLabel(raw), URL: raw})
		if len(out) >= discogsLinksCap {
			break
		}
	}
	return out
}

func linkLabel(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "Link"
	}
	host := strings.ToLower(u.Host)
	for _, h := range discogsLinkHosts {
		if strings.Contains(host, h.host) {
			return h.label
		}
	}
	return strings.TrimPrefix(host, "www.")
}

// discogsMarkup matches the Discogs profile BBCode we strip for display:
// [b]/[i] toggles, [url=…]/[/url] wrappers, and id-only refs ([a123]/[l456]).
var discogsMarkup = regexp.MustCompile(`\[/?[bi]\]|\[url=[^\]]*\]|\[/url\]|\[[almb]\d+\]`)

// discogsNamedRef matches a named entity ref ([a=Name]/[l=Label]/[m=Name]),
// keeping the inner name.
var discogsNamedRef = regexp.MustCompile(`\[[alm]=([^\]]+)\]`)

// cleanDiscogsProfile strips Discogs BBCode markup from a profile, keeping named
// entity references' display text and dropping bare-id refs and formatting tags.
func cleanDiscogsProfile(profile string) string {
	if profile == "" {
		return ""
	}
	out := discogsNamedRef.ReplaceAllString(profile, "$1")
	out = discogsMarkup.ReplaceAllString(out, "")
	return strings.TrimSpace(out)
}

// --- artist enrichment response shapes (verified 2026-06-22) ---

type discogsArtistRef struct {
	Name string `json:"name"`
}

type discogsArtistFull struct {
	Profile        string             `json:"profile"`
	RealName       string             `json:"realname"`
	NameVariations []string           `json:"namevariations"`
	Aliases        []discogsArtistRef `json:"aliases"`
	Members        []discogsArtistRef `json:"members"`
	Groups         []discogsArtistRef `json:"groups"`
	URLs           []string           `json:"urls"`
}

// --- enrichment response shapes (verified 2026-06-22, docs/providers/discogs.md §4) ---

type discogsExtraArtist struct {
	Name string `json:"name"`
	Role string `json:"role"`
	ID   int    `json:"id"`
}

type discogsTrack struct {
	Title        string               `json:"title"`
	ExtraArtists []discogsExtraArtist `json:"extraartists"`
}

type discogsMaster struct {
	ID          int            `json:"id"`
	Year        int            `json:"year"`
	Genres      []string       `json:"genres"`
	Styles      []string       `json:"styles"`
	MainRelease int            `json:"main_release"`
	Tracklist   []discogsTrack `json:"tracklist"`
}

type discogsLabelRef struct {
	Name  string `json:"name"`
	Catno string `json:"catno"`
}

type discogsCompany struct {
	Name           string `json:"name"`
	EntityTypeName string `json:"entity_type_name"`
}

type discogsFormat struct {
	Name         string   `json:"name"`
	Descriptions []string `json:"descriptions"`
}

type discogsCommunity struct {
	Have   int `json:"have"`
	Want   int `json:"want"`
	Rating struct {
		Count   int     `json:"count"`
		Average float64 `json:"average"`
	} `json:"rating"`
}

type discogsReleaseDetail struct {
	Country      string               `json:"country"`
	Genres       []string             `json:"genres"`
	Styles       []string             `json:"styles"`
	ExtraArtists []discogsExtraArtist `json:"extraartists"`
	Labels       []discogsLabelRef    `json:"labels"`
	Companies    []discogsCompany     `json:"companies"`
	Formats      []discogsFormat      `json:"formats"`
	Community    discogsCommunity     `json:"community"`
}
