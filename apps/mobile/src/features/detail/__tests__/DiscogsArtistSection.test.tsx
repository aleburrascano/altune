/**
 * DiscogsArtistSection — the Discogs artist metadata block (docs/providers/discogs.md cap 7).
 *
 * Pure presentational: renders bio, real name, aliases, groups, and tappable
 * external links; nothing when null. useTheme defaults to the dark theme without
 * a provider, so it renders bare. Linking is mocked to assert link taps.
 */
import { fireEvent, render } from '@testing-library/react-native';
import { createElement } from 'react';
import { Linking } from 'react-native';

import { DiscogsArtistSection } from '../ui/DiscogsArtistSection';
import type { DiscogsArtistEnrichmentResponse } from '../../../shared/api-client/discovery';

function _enrichment(
  over: Partial<DiscogsArtistEnrichmentResponse> = {},
): DiscogsArtistEnrichmentResponse {
  return {
    artist_id: 3062364,
    profile: 'American rapper and songwriter.',
    real_name: 'Kendrick Lamar Duckworth',
    aliases: ['K Dot (2)', 'OKLAMA'],
    name_variations: [],
    members: [],
    groups: ['Black Hippy'],
    links: [
      { label: 'Wikipedia', url: 'https://en.wikipedia.org/wiki/Kendrick_Lamar' },
      { label: 'Instagram', url: 'https://instagram.com/kendricklamar' },
    ],
    ...over,
  };
}

describe('DiscogsArtistSection', () => {
  it('renders bio, groups, and links', () => {
    const { getByTestId } = render(
      createElement(DiscogsArtistSection, { enrichment: _enrichment() }),
    );
    expect(getByTestId('detail-discogs-artist')).toBeTruthy();
    expect(getByTestId('detail-discogs-bio')).toBeTruthy();
    expect(getByTestId('detail-discogs-groups')).toBeTruthy();
    expect(getByTestId('detail-discogs-link-0')).toBeTruthy();
    expect(getByTestId('detail-discogs-link-1')).toBeTruthy();
  });

  it('opens the external URL when a link is tapped', () => {
    const spy = jest.spyOn(Linking, 'openURL').mockResolvedValue(undefined as never);
    const { getByTestId } = render(
      createElement(DiscogsArtistSection, { enrichment: _enrichment() }),
    );

    fireEvent.press(getByTestId('detail-discogs-link-0'));
    expect(spy).toHaveBeenCalledWith('https://en.wikipedia.org/wiki/Kendrick_Lamar');
    spy.mockRestore();
  });

  it('renders nothing when enrichment is null', () => {
    const { queryByTestId } = render(
      createElement(DiscogsArtistSection, { enrichment: null }),
    );
    expect(queryByTestId('detail-discogs-artist')).toBeNull();
  });
});
