import fc from 'fast-check';
import { QueryClient } from '@tanstack/react-query';

import { discoveryKeys, libraryKeys, playlistKeys } from '../query-keys';

function makeClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

describe('libraryKeys — literal shape', () => {
  it('summary is the fixed two-segment key', () => {
    expect(libraryKeys.summary).toEqual(['library', 'summary']);
  });

  it('tracksPrefix is the fixed two-segment prefix', () => {
    expect(libraryKeys.tracksPrefix).toEqual(['library', 'tracks']);
  });

  it('tracks(query, sort) appends query then sort, in that order', () => {
    expect(libraryKeys.tracks('rock', 'title')).toEqual(['library', 'tracks', 'rock', 'title']);
  });

  it('lookupPrefix is the fixed two-segment prefix', () => {
    expect(libraryKeys.lookupPrefix).toEqual(['library', 'lookup']);
  });

  it('lookup(query) appends the query as the third segment', () => {
    expect(libraryKeys.lookup('needle')).toEqual(['library', 'lookup', 'needle']);
  });

  it('albumsPrefix is the fixed two-segment prefix', () => {
    expect(libraryKeys.albumsPrefix).toEqual(['library', 'albums']);
  });

  it('albums(query, sort) appends query then sort, in that order', () => {
    expect(libraryKeys.albums('beatles', 'recent')).toEqual([
      'library',
      'albums',
      'beatles',
      'recent',
    ]);
  });

  it('artistsPrefix is the fixed two-segment prefix', () => {
    expect(libraryKeys.artistsPrefix).toEqual(['library', 'artists']);
  });

  it('artists(query, sort) appends query then sort, in that order', () => {
    expect(libraryKeys.artists('beatles', 'recent')).toEqual([
      'library',
      'artists',
      'beatles',
      'recent',
    ]);
  });

  it('featuringPrefix is the fixed two-segment prefix', () => {
    expect(libraryKeys.featuringPrefix).toEqual(['library', 'featuring']);
  });

  it('featuring(identity) appends the identity as the third segment', () => {
    expect(libraryKeys.featuring('artist-42')).toEqual(['library', 'featuring', 'artist-42']);
  });
});

describe('discoveryKeys — literal shape', () => {
  it('history is the fixed two-segment key', () => {
    expect(discoveryKeys.history).toEqual(['discovery', 'history']);
  });

  it('searchPrefix is the fixed two-segment prefix', () => {
    expect(discoveryKeys.searchPrefix).toEqual(['discovery', 'search']);
  });

  it('search(query) appends the query as the third segment', () => {
    expect(discoveryKeys.search('daft punk')).toEqual(['discovery', 'search', 'daft punk']);
  });

  it('suggest(query) appends the query under its own "suggest" segment, distinct from search', () => {
    expect(discoveryKeys.suggest('daft punk')).toEqual(['discovery', 'suggest', 'daft punk']);
  });

  it('lyrics(title, artist) appends title then artist, in that order, as two distinct segments', () => {
    expect(discoveryKeys.lyrics('Get Lucky', 'Daft Punk')).toEqual([
      'discovery',
      'lyrics',
      'Get Lucky',
      'Daft Punk',
    ]);
  });
});

describe('playlistKeys — literal shape', () => {
  it('list is the singleton "playlists" key', () => {
    expect(playlistKeys.list).toEqual(['playlists']);
  });

  it('details is the singleton "playlist" prefix, spelled differently from list', () => {
    expect(playlistKeys.details).toEqual(['playlist']);
  });

  it('detail(playlistId) appends the id under the "playlist" segment', () => {
    expect(playlistKeys.detail('pl-1')).toEqual(['playlist', 'pl-1']);
  });
});

describe('query-key factories — arguments a real screen can produce, for the five never-exercised factories', () => {
  it.each<[string, string]>([
    ['', ''],
    [' ', ' '],
    ['日本語', 'アーティスト'],
    ['a'.repeat(2000), 'b'.repeat(2000)],
  ])('albums(%j, %j) keeps both arguments verbatim, in position', (query, sort) => {
    expect(libraryKeys.albums(query, sort)).toEqual(['library', 'albums', query, sort]);
  });

  it.each<[string, string]>([
    ['', ''],
    [' ', ' '],
    ['日本語', 'アーティスト'],
    ['a'.repeat(2000), 'b'.repeat(2000)],
  ])('artists(%j, %j) keeps both arguments verbatim, in position', (query, sort) => {
    expect(libraryKeys.artists(query, sort)).toEqual(['library', 'artists', query, sort]);
  });

  it.each<[string]>([[''], [' '], ['日本語検索'], ['a'.repeat(2000)]])(
    'search(%j) keeps the argument verbatim',
    (query) => {
      expect(discoveryKeys.search(query)).toEqual(['discovery', 'search', query]);
    },
  );

  it.each<[string]>([[''], [' '], ['日本語検索'], ['a'.repeat(2000)]])(
    'suggest(%j) keeps the argument verbatim',
    (query) => {
      expect(discoveryKeys.suggest(query)).toEqual(['discovery', 'suggest', query]);
    },
  );

  it.each<[string, string]>([
    ['', ''],
    [' ', ' '],
    ['歌詞', 'アーティスト'],
    ['a'.repeat(2000), 'b'.repeat(2000)],
  ])('lyrics(%j, %j) keeps both arguments as two distinct segments, never joined', (title, artist) => {
    expect(discoveryKeys.lyrics(title, artist)).toEqual(['discovery', 'lyrics', title, artist]);
  });
});

describe('law: every <x>Prefix is a strict prefix of its <x>(...) factory output', () => {
  const singleArgPairs = [
    { name: 'lookup', prefix: libraryKeys.lookupPrefix, factory: libraryKeys.lookup },
    { name: 'featuring', prefix: libraryKeys.featuringPrefix, factory: libraryKeys.featuring },
    { name: 'search', prefix: discoveryKeys.searchPrefix, factory: discoveryKeys.search },
  ];

  const twoArgPairs = [
    { name: 'tracks', prefix: libraryKeys.tracksPrefix, factory: libraryKeys.tracks },
    { name: 'albums', prefix: libraryKeys.albumsPrefix, factory: libraryKeys.albums },
    { name: 'artists', prefix: libraryKeys.artistsPrefix, factory: libraryKeys.artists },
  ];

  it.each(singleArgPairs)(
    '$name: the prefix leads the factory output for any string argument',
    ({ prefix, factory }) => {
      fc.assert(
        fc.property(fc.string(), (arg) => {
          const output: readonly unknown[] = factory(arg);
          expect(output.slice(0, prefix.length)).toEqual(prefix);
          expect(output.length).toBeGreaterThan(prefix.length);
        }),
      );
    },
  );

  it.each(twoArgPairs)(
    '$name: the prefix leads the factory output for any pair of string arguments',
    ({ prefix, factory }) => {
      fc.assert(
        fc.property(fc.string(), fc.string(), (a, b) => {
          const output: readonly unknown[] = factory(a, b);
          expect(output.slice(0, prefix.length)).toEqual(prefix);
          expect(output.length).toBeGreaterThan(prefix.length);
        }),
      );
    },
  );
});

describe('invalidation — the real QueryClient prefix matcher, and no others', () => {
  it('tracksPrefix reaches tracks(q, sort) for arbitrary q/sort', async () => {
    const client = makeClient();
    client.setQueryData(libraryKeys.tracks('rock', 'title'), { items: ['a'] });
    client.setQueryData(libraryKeys.tracks('', 'recent'), { items: ['b'] });

    await client.invalidateQueries({ queryKey: libraryKeys.tracksPrefix });

    expect(client.getQueryState(libraryKeys.tracks('rock', 'title'))?.isInvalidated).toBe(true);
    expect(client.getQueryState(libraryKeys.tracks('', 'recent'))?.isInvalidated).toBe(true);
  });

  it('albumsPrefix reaches albums(...) but not artists(...) — a one-segment difference', async () => {
    const client = makeClient();
    client.setQueryData(libraryKeys.albums('q', 'recent'), { items: ['album'] });
    client.setQueryData(libraryKeys.artists('q', 'recent'), { items: ['artist'] });

    await client.invalidateQueries({ queryKey: libraryKeys.albumsPrefix });

    expect(client.getQueryState(libraryKeys.albums('q', 'recent'))?.isInvalidated).toBe(true);
    expect(client.getQueryState(libraryKeys.artists('q', 'recent'))?.isInvalidated).toBe(false);
  });

  it('searchPrefix reaches search(q) but not suggest(q)', async () => {
    const client = makeClient();
    client.setQueryData(discoveryKeys.search('daft punk'), { items: ['result'] });
    client.setQueryData(discoveryKeys.suggest('daft punk'), { items: ['suggestion'] });

    await client.invalidateQueries({ queryKey: discoveryKeys.searchPrefix });

    expect(client.getQueryState(discoveryKeys.search('daft punk'))?.isInvalidated).toBe(true);
    expect(client.getQueryState(discoveryKeys.suggest('daft punk'))?.isInvalidated).toBe(false);
  });

  it('playlistKeys.details reaches playlistKeys.detail(id), and playlistKeys.list does not', async () => {
    const client = makeClient();
    client.setQueryData(playlistKeys.detail('pl-1'), { id: 'pl-1' });
    client.setQueryData(playlistKeys.list, { items: [{ id: 'pl-1' }] });

    await client.invalidateQueries({ queryKey: playlistKeys.details });

    expect(client.getQueryState(playlistKeys.detail('pl-1'))?.isInvalidated).toBe(true);
    expect(client.getQueryState(playlistKeys.list)?.isInvalidated).toBe(false);
  });

  it('libraryKeys.summary is not reached by tracksPrefix — summary and tracks are siblings under library', async () => {
    const client = makeClient();
    client.setQueryData(libraryKeys.summary, { total: 0 });

    await client.invalidateQueries({ queryKey: libraryKeys.tracksPrefix });

    expect(client.getQueryState(libraryKeys.summary)?.isInvalidated).toBe(false);
  });
});

type Namespace = Record<string, unknown>;

function sampleOutputs(namespace: Namespace): (readonly unknown[])[] {
  return Object.values(namespace).map((value) => {
    if (typeof value === 'function') {
      const arity = value.length;
      const args = Array.from({ length: arity }, (_unused, i) => `sample-arg-${i}`);
      return value(...args) as readonly unknown[];
    }
    return value as readonly unknown[];
  });
}

function isPrefixOrEqual(a: readonly unknown[], b: readonly unknown[]): boolean {
  const shorter = Math.min(a.length, b.length);
  for (let i = 0; i < shorter; i += 1) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}

describe('invariant — the three namespaces never collide', () => {
  const namespaces: [string, Namespace][] = [
    ['libraryKeys', libraryKeys],
    ['discoveryKeys', discoveryKeys],
    ['playlistKeys', playlistKeys],
  ];

  it.each([
    ['libraryKeys', 'discoveryKeys'],
    ['libraryKeys', 'playlistKeys'],
    ['discoveryKeys', 'playlistKeys'],
  ])('no key or factory output in %s is a prefix of, or equal to, any output in %s', (nameA, nameB) => {
    const outputsA = sampleOutputs(namespaces.find(([n]) => n === nameA)![1]);
    const outputsB = sampleOutputs(namespaces.find(([n]) => n === nameB)![1]);

    for (const outA of outputsA) {
      for (const outB of outputsB) {
        expect(isPrefixOrEqual(outA, outB)).toBe(false);
        expect(isPrefixOrEqual(outB, outA)).toBe(false);
      }
    }
  });
});
