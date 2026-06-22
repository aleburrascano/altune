/**
 * DiscogsEnrichmentSection — the Discogs liner-notes block (docs/providers/discogs.md).
 *
 * Pure presentational: renders style pills, role-grouped credits, label/catalog,
 * and community signal from an enrichment prop; nothing when null. useTheme
 * defaults to the dark theme without a provider, so it renders bare.
 */
import { render } from '@testing-library/react-native';
import { createElement } from 'react';

import {
  DiscogsEnrichmentSection,
  groupCreditsByRole,
} from '../ui/DiscogsEnrichmentSection';
import type { DiscogsEnrichmentResponse } from '../../../shared/api-client/discovery';

function _enrichment(over: Partial<DiscogsEnrichmentResponse> = {}): DiscogsEnrichmentResponse {
  return {
    master_id: 1164779,
    genres: ['Hip Hop'],
    styles: ['Conscious', 'Trap'],
    year: 2017,
    credits: [
      { name: 'Bēkon', role: 'Producer' },
      { name: 'Sounwave', role: 'Producer' },
      { name: 'Kendrick Duckworth', role: 'Written-By' },
    ],
    labels: [{ name: 'Top Dawg Entertainment', catno: 'B0026716-02' }],
    formats: ['CD · Album'],
    country: 'US',
    companies: [],
    community: { have: 2980, want: 1946, rating: 4.27, votes: 313 },
    ...over,
  };
}

describe('DiscogsEnrichmentSection', () => {
  it('renders styles, credits, label, and community', () => {
    const { getByTestId } = render(
      createElement(DiscogsEnrichmentSection, { enrichment: _enrichment() }),
    );
    expect(getByTestId('detail-discogs')).toBeTruthy();
    expect(getByTestId('detail-discogs-style-0')).toBeTruthy();
    expect(getByTestId('detail-discogs-style-1')).toBeTruthy();
    expect(getByTestId('detail-discogs-credit-0')).toBeTruthy(); // Producer group
    expect(getByTestId('detail-discogs-credit-1')).toBeTruthy(); // Written-By group
    expect(getByTestId('detail-discogs-label')).toBeTruthy();
    expect(getByTestId('detail-discogs-community')).toBeTruthy();
  });

  it('renders nothing when enrichment is null', () => {
    const { queryByTestId } = render(
      createElement(DiscogsEnrichmentSection, { enrichment: null }),
    );
    expect(queryByTestId('detail-discogs')).toBeNull();
  });

  it('caps style pills at six', () => {
    const { getByTestId, queryByTestId } = render(
      createElement(DiscogsEnrichmentSection, {
        enrichment: _enrichment({ styles: ['a', 'b', 'c', 'd', 'e', 'f', 'g'] }),
      }),
    );
    expect(getByTestId('detail-discogs-style-5')).toBeTruthy();
    expect(queryByTestId('detail-discogs-style-6')).toBeNull();
  });
});

describe('groupCreditsByRole', () => {
  it('collapses credits into ordered role groups, preserving first-seen order', () => {
    const groups = groupCreditsByRole([
      { name: 'Bēkon', role: 'Producer' },
      { name: 'Sounwave', role: 'Producer' },
      { name: 'Kendrick Duckworth', role: 'Written-By' },
      { name: 'Bēkon', role: 'Producer' }, // dup name within role
    ]);
    expect(groups).toEqual([
      { role: 'Producer', names: ['Bēkon', 'Sounwave'] },
      { role: 'Written-By', names: ['Kendrick Duckworth'] },
    ]);
  });

  it('returns an empty array for no credits', () => {
    expect(groupCreditsByRole([])).toEqual([]);
  });
});
