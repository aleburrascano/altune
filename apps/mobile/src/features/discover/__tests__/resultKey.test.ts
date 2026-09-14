import { resultKey } from '../resultKey';
import { resultFixture } from './fixtures';

describe('resultKey builds a stable key with source-aware fallbacks', () => {
  it('uses kind, first provider and external id when a source with an id is present', () => {
    const key = resultKey(
      resultFixture({
        kind: 'album',
        sources: [{ provider: 'tidal', external_id: 'abc', url: 'u' }],
      }),
      3,
    );

    expect(key).toBe('album-tidal-abc');
  });

  it('falls back to title and index when the first source has an empty external id', () => {
    const key = resultKey(
      resultFixture({
        kind: 'track',
        title: 'Karma Police',
        sources: [{ provider: 'spotify', external_id: '', url: 'u' }],
      }),
      2,
    );

    expect(key).toBe('track-spotify-Karma Police-2');
  });

  it('falls back to a placeholder provider and title-index when there is no source', () => {
    const key = resultKey(resultFixture({ kind: 'artist', title: 'Thom Yorke', sources: [] }), 5);

    expect(key).toBe('artist-x-Thom Yorke-5');
  });
});
