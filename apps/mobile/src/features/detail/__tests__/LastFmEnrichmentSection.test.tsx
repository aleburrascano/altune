import { render } from '@testing-library/react-native';
import { createElement } from 'react';

import { LastFmEnrichmentSection } from '../ui/LastFmEnrichmentSection';
import type { LastFmEnrichmentResponse } from '../../../shared/api-client/enrichment';
import type { DiscoveryKind } from '../../../shared/api-client/discovery';

function _enrichment(over: Partial<LastFmEnrichmentResponse> = {}): LastFmEnrichmentResponse {
  return {
  has_content: true,
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
  it('leaves genres and listener counts to the banner and the fact row', () => {
    const { getByTestId, queryByTestId } = _render('artist', _enrichment());
    expect(getByTestId('detail-lastfm')).toBeTruthy();
    expect(queryByTestId('detail-lastfm-popularity')).toBeNull();
    expect(queryByTestId('detail-lastfm-tag-0')).toBeNull();
  });

  it('shows the bio and similar artists as rows for an artist (Editorial About)', () => {
    const { getByTestId } = _render('artist', _enrichment());
    expect(getByTestId('detail-lastfm-bio')).toHaveTextContent('A song blurb.');
    expect(getByTestId('detail-lastfm-similar')).toBeTruthy();
    expect(getByTestId('detail-lastfm-similar-0')).toHaveTextContent('Baby Keem');
    expect(getByTestId('detail-lastfm-similar-1')).toHaveTextContent('Jay Rock');
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
  has_content: true,
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
