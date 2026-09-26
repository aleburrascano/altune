// Probe (#2819): buildAlbumFacts is pure now it is out of AlbumDetailBody. The
// body characterization pins one track with a year; these pin the edges: summed
// runtime, tracks with no duration, and no year.

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { buildAlbumFacts } from '../ui/albumDetailFacts';

function track(title: string, durationSeconds?: number): DiscoveryResult {
  return {
    kind: 'track',
    title,
    subtitle: 'Fleetwood Mac',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: durationSeconds === undefined ? {} : { duration_seconds: durationSeconds },
  };
}

function shownValues(tracks: DiscoveryResult[], year: string | null): string[] {
  return buildAlbumFacts(tracks, year)
    .filter((fact): fact is NonNullable<typeof fact> => fact !== null)
    .map((fact) => fact.value);
}

describe('buildAlbumFacts', () => {
  it('adds every track into one runtime', () => {
    const values = shownValues([track('Dreams', 1800), track('Songbird', 1860)], '1977');

    expect(values).toHaveLength(3);
    expect(values).toEqual(expect.arrayContaining(['2', '1 hr 1 min', '1977']));
  });

  it('counts a track with no duration but adds nothing to the runtime', () => {
    const values = shownValues([track('Dreams', 125), track('Songbird')], '1977');

    expect(values).toHaveLength(3);
    expect(values).toEqual(expect.arrayContaining(['2', '2 min', '1977']));
  });

  it('hides the runtime when no track has a duration', () => {
    const values = shownValues([track('Dreams'), track('Songbird')], '1977');

    expect(values).toHaveLength(2);
    expect(values).toEqual(expect.arrayContaining(['2', '1977']));
  });

  it('hides the released year when the year is unknown', () => {
    const values = shownValues([track('Dreams', 125)], null);

    expect(values).toHaveLength(2);
    expect(values).toEqual(expect.arrayContaining(['1', '2 min']));
  });
});
