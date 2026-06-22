/**
 * EnrichmentSection — the MusicBrainz metadata block (musicbrainz-enrichment spec).
 *
 * Pure presentational: renders genre pills + year/rating from an enrichment
 * prop, and nothing when the enrichment is null or carries no textual metadata.
 * useTheme defaults to the dark theme without a provider, so it renders bare.
 */
import { render } from '@testing-library/react-native';
import { createElement } from 'react';

import { EnrichmentSection } from '../ui/EnrichmentSection';
import type { EnrichmentResponse } from '../../../shared/api-client/discovery';

function _enrichment(over: Partial<EnrichmentResponse> = {}): EnrichmentResponse {
  return {
    mbid: 'mbid-1',
    genres: ['conscious hip hop', 'hip hop'],
    year: 2017,
    rating: 4.1,
    rating_votes: 7,
    primary_type: 'Album',
    secondary_types: [],
    external_ids: {},
    artwork_url: '',
    ...over,
  };
}

describe('EnrichmentSection', () => {
  it('renders genre chips and the section when enrichment has genres', () => {
    const { getByTestId } = render(
      createElement(EnrichmentSection, { enrichment: _enrichment() }),
    );
    expect(getByTestId('detail-enrichment')).toBeTruthy();
    expect(getByTestId('detail-genre-0')).toBeTruthy();
    expect(getByTestId('detail-genre-1')).toBeTruthy();
  });

  it('caps the rendered genres at four', () => {
    const { getByTestId, queryByTestId } = render(
      createElement(EnrichmentSection, {
        enrichment: _enrichment({ genres: ['a', 'b', 'c', 'd', 'e', 'f'] }),
      }),
    );
    expect(getByTestId('detail-genre-3')).toBeTruthy();
    expect(queryByTestId('detail-genre-4')).toBeNull();
  });

  it('renders nothing when enrichment is null', () => {
    const { queryByTestId } = render(
      createElement(EnrichmentSection, { enrichment: null }),
    );
    expect(queryByTestId('detail-enrichment')).toBeNull();
  });

  it('renders nothing when enrichment has no textual metadata', () => {
    const { queryByTestId } = render(
      createElement(EnrichmentSection, {
        enrichment: _enrichment({
          genres: [],
          year: 0,
          rating: 0,
          artwork_url: 'https://caa/x.jpg',
          external_ids: { deezer: '1' },
        }),
      }),
    );
    expect(queryByTestId('detail-enrichment')).toBeNull();
  });
});
