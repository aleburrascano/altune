import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedTrack } from '@shared/apiClient/enrichment';

import { isAcquirable, unreleasedAsResult } from '@features/detail/domain/unreleasedTrack';

const ARTIST: DiscoveryResult = {
  kind: 'artist',
  title: 'Ken Carson',
  subtitle: null,
  image_url: null,
  confidence: 'high',
  sources: [],
  extras: {},
};

const DREAM: UnreleasedTrack = {
  genius_id: 1,
  title: 'Dream',
  cover_url: 'https://img.example/d.jpg',
  genius_url: 'https://genius.com/d',
  release_date: null,
  has_media: true,
  audio_links: [],
  extras: { owned_track_id: 'track-9' },
};

const YOUTUBE_URL = 'https://www.youtube.com/watch?v=dQw4w9WgXcQ';
const SOUNDCLOUD_URL = 'https://soundcloud.com/ken/dream';

describe('unreleasedAsResult', () => {
  it('saves under the artist page, album UNRELEASED, keeping the ownership extras', () => {
    const result = unreleasedAsResult(DREAM, ARTIST);

    expect(result).toMatchObject({ kind: 'track', title: 'Dream', subtitle: 'Ken Carson' });
    expect(result.extras).toEqual({ owned_track_id: 'track-9', album: 'UNRELEASED' });
  });

  it('pins a SoundCloud link by its url and a YouTube link by its video id', () => {
    const links: UnreleasedTrack['audio_links'] = [
      { provider: 'soundcloud', url: SOUNDCLOUD_URL },
      { provider: 'youtube', url: YOUTUBE_URL },
    ];

    const { sources } = unreleasedAsResult({ ...DREAM, audio_links: links }, ARTIST);

    expect(sources).toEqual([
      { provider: 'soundcloud', external_id: SOUNDCLOUD_URL, url: SOUNDCLOUD_URL },
      { provider: 'youtube', external_id: 'dQw4w9WgXcQ', url: YOUTUBE_URL },
    ]);
  });

  it('drops a YouTube link with no video id, leaving the song not acquirable', () => {
    const track = {
      ...DREAM,
      audio_links: [{ provider: 'youtube' as const, url: 'https://youtube.com/x' }],
    };

    expect(unreleasedAsResult(track, ARTIST).sources).toEqual([]);
    expect(isAcquirable(track)).toBe(false);
  });
});
