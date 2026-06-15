import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';

import {
  dedupAlbumsByTitle,
  mergedSources,
  sortByReleaseDateDesc,
} from '../helpers/artist-content';

function album(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'album',
    title: 'Album',
    subtitle: 'Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
    ...overrides,
  };
}

function source(provider: string, externalId: string): DiscoverySource {
  return { provider, external_id: externalId, url: '' };
}

describe('mergedSources', () => {
  it('merges unique sources', () => {
    const a = [source('deezer', '1')];
    const b = [source('musicbrainz', '2')];
    expect(mergedSources(a, b)).toHaveLength(2);
  });

  it('deduplicates by provider:external_id', () => {
    const a = [source('deezer', '1')];
    const b = [source('deezer', '1')];
    expect(mergedSources(a, b)).toHaveLength(1);
  });

  it('preserves order: a first, then unique from b', () => {
    const a = [source('deezer', '1')];
    const b = [source('musicbrainz', '2'), source('deezer', '1')];
    const result = mergedSources(a, b);
    expect(result[0]!.provider).toBe('deezer');
    expect(result[1]!.provider).toBe('musicbrainz');
  });
});

describe('sortByReleaseDateDesc', () => {
  it('sorts by release_date descending', () => {
    const albums = [
      album({ title: 'Old', extras: { release_date: '2020-01-01' } }),
      album({ title: 'New', extras: { release_date: '2024-06-15' } }),
    ];
    const result = sortByReleaseDateDesc(albums);
    expect(result[0]!.title).toBe('New');
  });

  it('falls back to year when release_date is missing', () => {
    const albums = [
      album({ title: 'Old', extras: { year: 2018 } }),
      album({ title: 'New', extras: { year: 2024 } }),
    ];
    const result = sortByReleaseDateDesc(albums);
    expect(result[0]!.title).toBe('New');
  });

  it('pushes null dates to the end', () => {
    const albums = [
      album({ title: 'No Date', extras: {} }),
      album({ title: 'Dated', extras: { release_date: '2024-01-01' } }),
    ];
    const result = sortByReleaseDateDesc(albums);
    expect(result[0]!.title).toBe('Dated');
    expect(result[1]!.title).toBe('No Date');
  });

  it('does not mutate the input array', () => {
    const albums = [album()];
    const result = sortByReleaseDateDesc(albums);
    expect(result).not.toBe(albums);
  });
});

describe('dedupAlbumsByTitle', () => {
  it('deduplicates by normalized title', () => {
    const albums = [
      album({ title: 'My Album', sources: [source('deezer', '1')] }),
      album({ title: 'my album', sources: [source('mb', '2')] }),
    ];
    const result = dedupAlbumsByTitle(albums);
    expect(result).toHaveLength(1);
  });

  it('keeps the album with higher track_count', () => {
    const albums = [
      album({ title: 'A', extras: { track_count: 5 } }),
      album({ title: 'a', extras: { track_count: 12 } }),
    ];
    const result = dedupAlbumsByTitle(albums);
    expect(result[0]!.extras['track_count']).toBe(12);
  });

  it('merges sources from duplicates', () => {
    const albums = [
      album({ title: 'A', sources: [source('deezer', '1')] }),
      album({ title: 'a', sources: [source('mb', '2')] }),
    ];
    const result = dedupAlbumsByTitle(albums);
    expect(result[0]!.sources).toHaveLength(2);
  });

  it('keeps the only instance when no duplicates', () => {
    const albums = [
      album({ title: 'Alpha' }),
      album({ title: 'Beta' }),
    ];
    expect(dedupAlbumsByTitle(albums)).toHaveLength(2);
  });
});
