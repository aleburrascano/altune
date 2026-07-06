/**
 * _withAlbumPositions — recovers real album track order for owned tracks that
 * were saved before track_number was sent, by matching them against the
 * authoritative (provider-ordered) album tracklist. Pure; no rendering.
 */
import { _withAlbumPositions } from '../hooks/useAlbumDetailState';
import type { DiscoveryResult } from '../../../shared/api-client/discovery';

function _track(title: string, extras: Record<string, unknown> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title,
    subtitle: 'Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras,
  };
}

const pos = (t: DiscoveryResult): number | undefined =>
  t.extras['track_position'] as number | undefined;

describe('_withAlbumPositions', () => {
  it('assigns positions from album order and sorts owned tracks by them', () => {
    const owned = [_track('Three'), _track('One')];
    const albumOrder = [_track('One'), _track('Two'), _track('Three')];

    const result = _withAlbumPositions(owned, albumOrder);

    expect(result.map((t) => t.title)).toEqual(['One', 'Three']);
    expect(pos(result[0]!)).toBe(1);
    expect(pos(result[1]!)).toBe(3);
  });

  it('honors an explicit track_position on the album track over its index', () => {
    const owned = [_track('B')];
    const albumOrder = [_track('A'), _track('B', { track_position: 7 })];

    const result = _withAlbumPositions(owned, albumOrder);

    expect(pos(result[0]!)).toBe(7);
  });

  it('keeps a position the owned track already carries (a newer save wins)', () => {
    const owned = [_track('One', { track_position: 5 })];
    const albumOrder = [_track('One'), _track('Two')];

    const result = _withAlbumPositions(owned, albumOrder);

    expect(pos(result[0]!)).toBe(5);
  });

  it('matches case/whitespace-insensitively', () => {
    const owned = [_track('  sicko MODE ')];
    const albumOrder = [_track('Stargazing'), _track('Sicko Mode')];

    const result = _withAlbumPositions(owned, albumOrder);

    expect(pos(result[0]!)).toBe(2);
  });

  it('sinks unmatched tracks to the end and leaves them position-less', () => {
    const owned = [_track('Ghost'), _track('One')];
    const albumOrder = [_track('One'), _track('Two')];

    const result = _withAlbumPositions(owned, albumOrder);

    expect(result.map((t) => t.title)).toEqual(['One', 'Ghost']);
    expect(pos(result[1]!)).toBeUndefined();
  });

  it('returns the input untouched while the tracklist is still empty (loading)', () => {
    const owned = [_track('One'), _track('Two')];

    const result = _withAlbumPositions(owned, []);

    expect(result).toBe(owned);
  });
});
