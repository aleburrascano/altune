import fc from 'fast-check';

import { getLibraryAlbums } from '../library';
import type { LibrarySort } from '../library';
import { searchDiscovery, suggestDiscovery, listSearchHistory } from '../discovery';
import type { DiscoveryKind, DiscoverySearchResponse } from '../discovery';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

function minimalSearchResponse(): DiscoverySearchResponse {
  return {
    query: 'q',
    query_norm: 'q',
    results: [],
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 0,
    offset: 0,
    has_more: false,
  };
}

function questionMarkCount(url: string): number {
  return (url.match(/\?/g) ?? []).length;
}

const wildString = fc.oneof(
  fc.constant(''),
  fc.string({ maxLength: 40 }),
  fc.string({
    unit: fc.constantFrom('&', '=', '?', '#', '+', '%', ' ', '\t', '\n', 'é', '日', '🎵'),
    minLength: 1,
    maxLength: 30,
  }),
  fc.string({ minLength: 300, maxLength: 600 }),
);

describe('law: libraryQueryString (via getLibraryAlbums) round-trips exactly through URLSearchParams', () => {
  it('no bare "?" when nothing is set, no key for an omitted/falsy value, exactly one "?" otherwise, values decode back exactly', async () => {
    __http.reply('GET /v1/library/albums', { status: 200, json: { items: [], total: 0 } });

    await fc.assert(
      fc.asyncProperty(
        fc.option(wildString, { nil: undefined }),
        fc.option(fc.constantFrom<LibrarySort>('recent', 'az', 'year'), { nil: undefined }),
        async (q, sort) => {
          await getLibraryAlbums({
            ...(q !== undefined ? { q } : {}),
            ...(sort !== undefined ? { sort } : {}),
          });

          const url = __http.last().url;
          const query = __http.last().query;
          const expectQ = q ? q : null;
          const expectSort = sort ? sort : null;

          if (expectQ === null && expectSort === null) {
            expect(url.endsWith('?')).toBe(false);
            expect(query).toBe('');
            return;
          }

          expect(questionMarkCount(url)).toBe(1);
          const qp = new URLSearchParams(query);
          expect(qp.get('q')).toBe(expectQ);
          expect(qp.get('sort')).toBe(expectSort);
          expect([...qp.keys()].sort()).toEqual(
            [expectQ !== null ? 'q' : null, expectSort !== null ? 'sort' : null]
              .filter((k): k is string => k !== null)
              .sort(),
          );
        },
      ),
      { numRuns: 200 },
    );
  });
});

describe("law: searchDiscovery's query string round-trips exactly through URLSearchParams", () => {
  it('holds for every combination of q/kinds/limit/offset/saveHistory, including the exact value each guard turns on', async () => {
    __http.reply('GET /v1/discovery/search', { status: 200, json: minimalSearchResponse() });

    await fc.assert(
      fc.asyncProperty(
        wildString,
        fc.option(
          fc.array(fc.constantFrom<DiscoveryKind>('artist', 'album', 'track'), { maxLength: 3 }),
          { nil: undefined },
        ),
        fc.option(fc.integer({ min: 0, max: 500 }), { nil: undefined }),
        fc.option(fc.integer({ min: 0, max: 500 }), { nil: undefined }),
        fc.option(fc.boolean(), { nil: undefined }),
        async (q, kinds, limit, offset, saveHistory) => {
          await searchDiscovery({
            q,
            ...(kinds !== undefined ? { kinds } : {}),
            ...(limit !== undefined ? { limit } : {}),
            ...(offset !== undefined ? { offset } : {}),
            ...(saveHistory !== undefined ? { saveHistory } : {}),
          });

          const url = __http.last().url;
          expect(questionMarkCount(url)).toBe(1);

          const qp = new URLSearchParams(__http.last().query);
          expect(qp.get('q')).toBe(q);

          const expectKinds = kinds && kinds.length > 0 ? kinds.join(',') : null;
          expect(qp.get('kinds')).toBe(expectKinds);

          const expectLimit = limit !== undefined ? String(limit) : null;
          expect(qp.get('limit')).toBe(expectLimit);

          const expectOffset = offset !== undefined && offset > 0 ? String(offset) : null;
          expect(qp.get('offset')).toBe(expectOffset);

          const expectSaveHistory = saveHistory === false ? 'false' : null;
          expect(qp.get('save_history')).toBe(expectSaveHistory);
        },
      ),
      { numRuns: 300 },
    );
  });
});

describe("law: suggestDiscovery's query string round-trips exactly through URLSearchParams", () => {
  it('holds for every q/limit combination', async () => {
    __http.reply('GET /v1/discovery/suggest', { status: 200, json: { suggestions: [] } });

    await fc.assert(
      fc.asyncProperty(
        wildString,
        fc.option(fc.integer({ min: 0, max: 500 }), { nil: undefined }),
        async (q, limit) => {
          await suggestDiscovery({
            q,
            ...(limit !== undefined ? { limit } : {}),
          });

          const url = __http.last().url;
          expect(questionMarkCount(url)).toBe(1);

          const qp = new URLSearchParams(__http.last().query);
          expect(qp.get('q')).toBe(q);
          expect(qp.get('limit')).toBe(limit !== undefined ? String(limit) : null);
        },
      ),
      { numRuns: 150 },
    );
  });
});

describe("law: listSearchHistory's query string round-trips exactly, including the bare-URL case", () => {
  it('holds for every limit, and produces no "?" at all when limit is omitted', async () => {
    __http.reply('GET /v1/discovery/search-history', {
      status: 200,
      json: { items: [], total: 0 },
    });

    await fc.assert(
      fc.asyncProperty(
        fc.option(fc.integer({ min: 0, max: 500 }), { nil: undefined }),
        async (limit) => {
          await listSearchHistory(limit === undefined ? undefined : { limit });

          const url = __http.last().url;

          if (limit === undefined) {
            expect(questionMarkCount(url)).toBe(0);
            expect(__http.last().query).toBe('');
            return;
          }

          expect(questionMarkCount(url)).toBe(1);
          const qp = new URLSearchParams(__http.last().query);
          expect(qp.get('limit')).toBe(String(limit));
        },
      ),
      { numRuns: 100 },
    );
  });
});
