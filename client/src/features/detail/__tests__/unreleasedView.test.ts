import type { UnreleasedAudioLink, UnreleasedTrack } from '@shared/apiClient/enrichment';

import { unreleasedView } from '@features/detail/domain/unreleasedView';

function track(key: string, over: Partial<UnreleasedTrack> = {}): UnreleasedTrack {
  return {
    key,
    versions: [],
    best_version_title: null,
    quality_rank: 0,
    genius_id: null,
    tracker_key: null,
    quality: null,
    title: key,
    cover_url: null,
    genius_url: null,
    release_date: null,
    has_media: null,
    off_streaming: true,
    audio_links: [],
    open_url: null,
    open_host: null,
    extras: {},
    ...over,
  };
}

const LINK: UnreleasedAudioLink = {
  provider: 'pillows',
  external_id: 'x',
  url: 'https://pillows.su/f/x',
};
const VERSION = { title: 'v' } as UnreleasedTrack['versions'][number];
const A = track('A', { quality_rank: 1, audio_links: [LINK], versions: [VERSION] });
const B = track('B', {
  quality_rank: 3,
  audio_links: [LINK],
  versions: [VERSION, VERSION, VERSION],
});
const C = track('C', { quality_rank: 0 });

function keys(list: UnreleasedTrack[]): string[] {
  return list.map((t) => t.key);
}

describe('unreleasedView', () => {
  it('best_quality, unreleased, playable gives B then A', () => {
    expect(keys(unreleasedView([A, B, C], 'best_quality', 'unreleased', 'playable'))).toEqual([
      'B',
      'A',
    ]);
  });

  it('popular keeps server order', () => {
    expect(keys(unreleasedView([A, B, C], 'popular', 'all', 'all'))).toEqual(['A', 'B', 'C']);
  });

  it('most_versions sorts by version count, ties keep server order', () => {
    const D = track('D');
    expect(keys(unreleasedView([C, A, D, B], 'most_versions', 'all', 'all'))).toEqual([
      'B',
      'A',
      'C',
      'D',
    ]);
  });

  it('newest sorts by date descending with undated last', () => {
    const old = track('old', { release_date: '2020-01-01' });
    const fresh = track('fresh', { release_date: '2024-05-06' });
    expect(keys(unreleasedView([C, old, fresh], 'newest', 'all', 'all'))).toEqual([
      'fresh',
      'old',
      'C',
    ]);
  });

  it('where released keeps only streaming tracks', () => {
    const live = track('live', { off_streaming: false });
    expect(keys(unreleasedView([A, live], 'popular', 'released', 'all'))).toEqual(['live']);
  });

  it('does not mutate the input', () => {
    const input = [A, B];
    unreleasedView(input, 'best_quality', 'all', 'all');
    expect(keys(input)).toEqual(['A', 'B']);
  });
});
