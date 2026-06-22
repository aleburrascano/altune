/**
 * LastFmEnrichmentSection — the Last.fm metadata block (docs/providers/lastfm.md cap 3).
 *
 * Pure presentational: renders compact popularity, tags, similar artists
 * (artist kind), and a bio/blurb (track & album kind). useTheme defaults to the
 * dark theme without a provider, so it renders bare.
 */
import { render } from '@testing-library/react-native';
import { createElement } from 'react';

import { LastFmEnrichmentSection } from '../ui/LastFmEnrichmentSection';
import type {
  DiscoveryKind,
  LastFmEnrichmentResponse,
} from '../../../shared/api-client/discovery';

function _enrichment(over: Partial<LastFmEnrichmentResponse> = {}): LastFmEnrichmentResponse {
  return {
    mbid: '381086ea',
    listeners: 5172275,
    playcount: 1050884806,
    tags: ['Hip-Hop', 'rap'],
    bio: 'A song blurb.',
    similar: ['Baby Keem', 'Jay Rock'],
    duration: 199,
    album: 'DAMN.',
    ...over,
  };
}

function _render(kind: DiscoveryKind, enrichment: LastFmEnrichmentResponse | null) {
  return render(createElement(LastFmEnrichmentSection, { kind, enrichment }));
}

describe('LastFmEnrichmentSection', () => {
  it('renders compact popularity and tags', () => {
    const { getByTestId } = _render('artist', _enrichment());
    expect(getByTestId('detail-lastfm')).toBeTruthy();
    expect(getByTestId('detail-lastfm-popularity')).toHaveTextContent(/5\.2M listeners/);
    expect(getByTestId('detail-lastfm-popularity')).toHaveTextContent(/1\.1B plays/);
    expect(getByTestId('detail-lastfm-tag-0')).toBeTruthy();
  });

  it('shows similar artists for an artist but not their bio (Discogs owns it)', () => {
    const { getByTestId, queryByTestId } = _render('artist', _enrichment());
    expect(getByTestId('detail-lastfm-similar')).toHaveTextContent(/Baby Keem, Jay Rock/);
    expect(queryByTestId('detail-lastfm-bio')).toBeNull();
  });

  it('shows the bio/blurb for a track but no similar-artist line', () => {
    const { getByTestId, queryByTestId } = _render('track', _enrichment());
    expect(getByTestId('detail-lastfm-bio')).toHaveTextContent('A song blurb.');
    expect(queryByTestId('detail-lastfm-similar')).toBeNull();
  });

  it('renders nothing when enrichment is null', () => {
    const { queryByTestId } = _render('album', null);
    expect(queryByTestId('detail-lastfm')).toBeNull();
  });

  it('renders nothing when there is no displayable content', () => {
    const { queryByTestId } = _render('artist', {
      mbid: '',
      listeners: 0,
      playcount: 0,
      tags: [],
      bio: '',
      similar: [],
      duration: 0,
      album: '',
    });
    expect(queryByTestId('detail-lastfm')).toBeNull();
  });
});
