package domain

import "altune/go-api/internal/shared/songfields"

func (r SearchResult) RecordingIdentity() songfields.RecordingIdentity {
	return songfields.RecordingIdentity{
		Title:            r.Title,
		Artist:           r.Subtitle,
		Album:            r.Album,
		Year:             r.Year,
		ReleaseDate:      r.ReleaseDate,
		RecordType:       string(r.RecordType),
		ISRC:             r.ISRC,
		RecordingMBID:    r.RecordingMBID,
		ReleaseMBID:      r.ReleaseMBID,
		ReleaseGroupMBID: r.ReleaseGroupMBID,
		ArtistMBID:       r.ArtistMBID,
		UPC:              r.UPC,
		DeezerAlbumID:    r.DeezerAlbumID,
		Position:         songfields.TrackPosition{Disc: r.TrackPosition.Disc, Track: r.TrackPosition.Track},
		TrackCount:       r.TrackCount,
		Contributors:     contributorsOf(r.Contributors),
		Explicit:         explicitOf(r.Explicit),
		DurationSeconds:  float64(r.Duration),
		Sources:          sourcesOf(r.Sources),
		Xref:             xrefOf(r.Xref),
	}
}

func contributorsOf(found []Contributor) []songfields.Contributor {
	if len(found) == 0 {
		return nil
	}
	out := make([]songfields.Contributor, len(found))
	for i, c := range found {
		out[i] = songfields.Contributor{ProviderID: c.ProviderID, Name: c.Name, Role: string(c.Role)}
	}
	return out
}

func explicitOf(flag *bool) *bool {
	if flag == nil {
		return nil
	}
	copied := *flag
	return &copied
}

func sourcesOf(links []ProviderLink) []songfields.ProviderRef {
	if len(links) == 0 {
		return nil
	}
	out := make([]songfields.ProviderRef, len(links))
	for i, l := range links {
		out[i] = songfields.ProviderRef{Provider: l.Provider.String(), ExternalID: l.ExternalID, URL: l.URL}
	}
	return out
}

func xrefOf(xref map[ProviderKey]string) map[string]string {
	if len(xref) == 0 {
		return nil
	}
	out := make(map[string]string, len(xref))
	for k, v := range xref {
		out[string(k)] = v
	}
	return out
}
