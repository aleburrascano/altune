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
  key: '1',
  versions: [],
  genius_id: 1,
  tracker_key: null,
  quality: null,
  title: 'Dream',
  cover_url: 'https://img.example/d.jpg',
  genius_url: 'https://genius.com/d',
  release_date: null,
  has_media: true,
  off_streaming: false,
  audio_links: [],
  open_url: null,
  open_host: null,
  extras: { owned_track_id: 'track-9' },
};

const SOUNDCLOUD_URL = 'https://soundcloud.com/ken/dream';
const PILLOWS_ID = '84310b5d2c21df85e4570aec299d1ead';
const PILLOWS_URL = `https://pillows.su/f/${PILLOWS_ID}`;

describe('unreleasedAsResult', () => {
  it('saves under the artist page, album UNRELEASED, keeping the ownership extras', () => {
    const result = unreleasedAsResult(DREAM, ARTIST);

    expect(result).toMatchObject({ kind: 'track', title: 'Dream', subtitle: 'Ken Carson' });
    expect(result.extras).toEqual({ owned_track_id: 'track-9', album: 'UNRELEASED' });
  });

  it('pins each link by the external id the server gave it', () => {
    const links: UnreleasedTrack['audio_links'] = [
      { provider: 'soundcloud', external_id: SOUNDCLOUD_URL, url: SOUNDCLOUD_URL },
      { provider: 'pillows', external_id: PILLOWS_ID, url: PILLOWS_URL },
    ];

    const track = { ...DREAM, audio_links: links };

    expect(unreleasedAsResult(track, ARTIST).sources).toEqual([
      { provider: 'soundcloud', external_id: SOUNDCLOUD_URL, url: SOUNDCLOUD_URL },
      { provider: 'pillows', external_id: PILLOWS_ID, url: PILLOWS_URL },
    ]);
    expect(isAcquirable(track)).toBe(true);
  });

  it('is not acquirable without a link', () => {
    expect(isAcquirable(DREAM)).toBe(false);
  });

  it('saves under the full title of the version whose links match the item', () => {
    const link = { provider: 'soundcloud', external_id: SOUNDCLOUD_URL, url: SOUNDCLOUD_URL };
    const other = { provider: 'pillows', external_id: PILLOWS_ID, url: PILLOWS_URL };
    const version = { label: null, quality: null, open_url: null, open_host: null, extras: {} };
    const track: UnreleasedTrack = {
      ...DREAM,
      title: 'Sports Car',
      audio_links: [link],
      versions: [
        { ...version, title: 'Sports Car [V1]', audio_links: [other] },
        { ...version, title: 'Sports Car [V10]', audio_links: [link] },
      ],
    };

    expect(unreleasedAsResult(track, ARTIST).title).toBe('Sports Car [V10]');
  });
});
