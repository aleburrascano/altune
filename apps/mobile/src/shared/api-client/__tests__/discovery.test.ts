import {
  searchDiscovery,
  suggestDiscovery,
  listSearchHistory,
  clearSearchHistory,
} from '../discovery';
import type { DiscoveryKind, DiscoveryResult, DiscoverySearchResponse } from '../discovery';
import { apiBase, ApiError } from '../index';
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

function discoveryResult(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: 'Fake Plastic Trees',
    subtitle: 'Radiohead',
    image_url: 'https://img.example/rh.png',
    confidence: 'high',
    sources: [{ provider: 'musicbrainz', external_id: 'mb-1', url: 'https://mb.example/1' }],
    extras: {},
    ...overrides,
  };
}

function fullSearchResponse(
  overrides: Partial<DiscoverySearchResponse> = {},
): DiscoverySearchResponse {
  return {
    query: 'radiohead',
    query_norm: 'radiohead',
    results: [discoveryResult()],
    sections: [{ kind: 'artist', items: [discoveryResult()], has_more: false }],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 1,
    offset: 0,
    has_more: false,
    ...overrides,
  };
}

function questionMarkCount(url: string): number {
  return (url.match(/\?/g) ?? []).length;
}

describe('Table: searchDiscovery query-string guards', () => {
  const cases: [string, Parameters<typeof searchDiscovery>[0], Record<string, string | null>][] = [
    [
      'kinds omitted -> no kinds param',
      { q: 'x' },
      { q: 'x', kinds: null, limit: null, offset: null, save_history: null },
    ],
    [
      'kinds empty array -> no kinds param (the guard is length > 0)',
      { q: 'x', kinds: [] },
      { q: 'x', kinds: null },
    ],
    ['kinds single value', { q: 'x', kinds: ['artist'] }, { q: 'x', kinds: 'artist' }],
    [
      'kinds multiple values joined with a comma',
      { q: 'x', kinds: ['artist', 'album', 'track'] as DiscoveryKind[] },
      { q: 'x', kinds: 'artist,album,track' },
    ],
    [
      'limit 0 is defined and IS sent (the guard is !== undefined)',
      { q: 'x', limit: 0 },
      { q: 'x', limit: '0' },
    ],
    ['limit omitted is not sent', { q: 'x' }, { q: 'x', limit: null }],
    [
      'offset 0 is the boundary and is NOT sent (the guard is > 0)',
      { q: 'x', offset: 0 },
      { q: 'x', offset: null },
    ],
    ['offset 1 is sent', { q: 'x', offset: 1 }, { q: 'x', offset: '1' }],
    ['offset omitted is not sent', { q: 'x' }, { q: 'x', offset: null }],
    [
      'saveHistory false is the only value that is sent (the guard is === false)',
      { q: 'x', saveHistory: false },
      { q: 'x', save_history: 'false' },
    ],
    ['saveHistory true is omitted', { q: 'x', saveHistory: true }, { q: 'x', save_history: null }],
    ['saveHistory omitted is omitted', { q: 'x' }, { q: 'x', save_history: null }],
    [
      "an empty-string q is still sent as q= -- unlike library.ts's truthy guard, this qs is unconditional",
      { q: '' },
      { q: '' },
    ],
  ];

  it.each(cases)('%s', async (_label, params, expected) => {
    __http.reply('GET /v1/discovery/search', { status: 200, json: fullSearchResponse() });

    await searchDiscovery(params);

    const url = __http.last().url;
    const qp = new URLSearchParams(__http.last().query);
    expect(questionMarkCount(url)).toBe(1);
    for (const [key, value] of Object.entries(expected)) {
      if (value === null) {
        expect(qp.has(key)).toBe(false);
      } else {
        expect(qp.get(key)).toBe(value);
      }
    }
  });
});

describe('AbortSignal threading through searchDiscovery (apiFetch substitutes its own composed deadline signal, so identity never matches -- only propagation proves the thread)', () => {
  it('a pre-aborted caller-supplied signal cancels the request before it ever resolves', async () => {
    __http.reply('GET /v1/discovery/search', { status: 200, json: fullSearchResponse() });
    const controller = new AbortController();
    controller.abort();

    await expect(searchDiscovery({ q: 'x' }, controller.signal)).rejects.toMatchObject({
      name: 'AbortError',
    });
  });

  it('aborting the caller-supplied signal mid-flight cancels a hung request', async () => {
    __http.hang('GET /v1/discovery/search');
    const controller = new AbortController();

    const pending = searchDiscovery({ q: 'x' }, controller.signal);
    controller.abort();

    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('with no caller-supplied signal, the request is not pre-aborted and completes normally', async () => {
    __http.reply('GET /v1/discovery/search', { status: 200, json: fullSearchResponse() });

    await searchDiscovery({ q: 'x' });

    expect(__http.last().signal.aborted).toBe(false);
  });
});

describe('response normalization', () => {
  it('normalizes a present top_result through normalizeResult (subtitle/image_url defaulted)', async () => {
    const raw = {
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
      top_result: {
        kind: 'artist',
        title: 'Radiohead',
        confidence: 'high',
        sources: [],
        extras: {},
      },
    };
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result.top_result).toEqual({
      kind: 'artist',
      title: 'Radiohead',
      confidence: 'high',
      sources: [],
      extras: {},
      subtitle: null,
      image_url: null,
    });
  });

  it('an absent top_result stays absent on the normalized response (no top_result key at all)', async () => {
    const raw = {
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
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result).not.toHaveProperty('top_result');
  });

  it('maps every item in every section through normalizeResult, defaulting an omitted-from-the-wire subtitle/image_url to null', async () => {
    const raw = {
      query: 'q',
      query_norm: 'q',
      results: [],
      sections: [
        {
          kind: 'album',
          has_more: true,
          items: [
            { kind: 'track', title: 'In Rainbows', confidence: 'high', sources: [], extras: {} },
          ],
        },
      ],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 0,
      offset: 0,
      has_more: false,
    };
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result.sections).toHaveLength(1);
    expect(result.sections[0]!.has_more).toBe(true);
    expect(result.sections[0]!.items[0]).toMatchObject({
      title: 'In Rainbows',
      subtitle: null,
      image_url: null,
    });
  });

  it('routes subtitle and image_url to their own keys on a top-level result rather than swapping them', async () => {
    const raw = fullSearchResponse({
      results: [
        discoveryResult({
          title: 'Just',
          subtitle: 'Radiohead',
          image_url: 'https://img.example/just.png',
        }),
      ],
      sections: [],
    });
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result.results[0]!.subtitle).toBe('Radiohead');
    expect(result.results[0]!.image_url).toBe('https://img.example/just.png');
  });

  it('keeps the server-reported total distinct from results.length on a real paginated page (500 total, a 20-item page)', async () => {
    const raw = fullSearchResponse({
      results: Array.from({ length: 20 }, (_, i) => discoveryResult({ title: `Track ${i}` })),
      sections: [],
      total: 500,
    });
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result.total).toBe(500);
    expect(result.results).toHaveLength(20);
  });
});

describe('Legacy/compat: an older server response missing newer fields', () => {
  function legacyResult(title: string): unknown {
    return {
      kind: 'track',
      title,
      subtitle: null,
      image_url: null,
      confidence: 'medium',
      sources: [],
      extras: {},
    };
  }

  it('omits total -> coerces to results.length (a full page), not zero', async () => {
    const raw = {
      query: 'q',
      query_norm: 'q',
      results: [legacyResult('a'), legacyResult('b'), legacyResult('c')],
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      offset: 0,
      has_more: false,
    };
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result.total).toBe(3);
    expect(result.results).toHaveLength(3);
  });

  it('omits both total and results (only sections carried) -> coerces to total 0 and results [] rather than throwing', async () => {
    const raw = {
      query: 'q',
      query_norm: 'q',
      sections: [{ kind: 'artist', has_more: false, items: [legacyResult('a')] }],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      offset: 0,
      has_more: false,
    };
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result.total).toBe(0);
    expect(result.results).toEqual([]);
  });

  it('omits offset -> coerces to 0', async () => {
    const raw = {
      query: 'q',
      query_norm: 'q',
      results: [legacyResult('a')],
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 1,
      has_more: false,
    };
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result.offset).toBe(0);
  });

  it('omits has_more -> coerces to false', async () => {
    const raw = {
      query: 'q',
      query_norm: 'q',
      results: [legacyResult('a')],
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 1,
      offset: 0,
    };
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result.has_more).toBe(false);
  });

  it('omits results entirely -> coerces to an empty array, not a crash', async () => {
    const raw = {
      query: 'q',
      query_norm: 'q',
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 0,
      offset: 0,
      has_more: false,
    };
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result.results).toEqual([]);
  });

  it('omits sections entirely -> coerces to an empty array, not a crash', async () => {
    const raw = {
      query: 'q',
      query_norm: 'q',
      results: [legacyResult('a')],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 1,
      offset: 0,
      has_more: false,
    };
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result.sections).toEqual([]);
  });

  it('a result with subtitle/image_url absent (Go omitempty) normalizes the same as explicit null', async () => {
    const raw = {
      query: 'q',
      query_norm: 'q',
      results: [
        { kind: 'track', title: 'Absent Fields', confidence: 'high', sources: [], extras: {} },
      ],
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 1,
      offset: 0,
      has_more: false,
    };
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result.results[0]!.subtitle).toBeNull();
    expect(result.results[0]!.image_url).toBeNull();
  });

  it('a result with explicit null subtitle/image_url normalizes the same way', async () => {
    const raw = {
      query: 'q',
      query_norm: 'q',
      results: [
        {
          kind: 'track',
          title: 'Explicit Nulls',
          subtitle: null,
          image_url: null,
          confidence: 'high',
          sources: [],
          extras: {},
        },
      ],
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 1,
      offset: 0,
      has_more: false,
    };
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result.results[0]!.subtitle).toBeNull();
    expect(result.results[0]!.image_url).toBeNull();
  });
});

describe('Adversarial: malformed/thin discovery payloads', () => {
  it('a DiscoveryResult missing sources passes through untouched (no runtime shape validation)', async () => {
    const raw = {
      query: 'q',
      query_norm: 'q',
      results: [
        {
          kind: 'track',
          title: 'No Sources',
          subtitle: null,
          image_url: null,
          confidence: 'high',
          extras: {},
        },
      ],
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 1,
      offset: 0,
      has_more: false,
    };
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    const result = await searchDiscovery({ q: 'q' });

    expect(result.results[0]).not.toHaveProperty('sources');
  });

  it('a null response body throws a TypeError (well-formed JSON, unguarded cast)', async () => {
    __http.reply('GET /v1/discovery/search', { status: 200, json: null });

    await expect(searchDiscovery({ q: 'q' })).rejects.toBeInstanceOf(TypeError);
  });

  const wrongTypeResultsCases: [string, unknown][] = [
    ['an object', { not: 'an array' }],
    ['a string', 'nope'],
    ['a number', 42],
  ];

  for (const [label, value] of wrongTypeResultsCases) {
    it(`results as ${label} throws a TypeError (well-formed JSON, unguarded cast)`, async () => {
      const raw = {
        query: 'q',
        query_norm: 'q',
        results: value,
        sections: [],
        providers: [],
        partial: false,
        cache: { hit: false, fetched_at: null },
        total: 0,
        offset: 0,
        has_more: false,
      };
      __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

      await expect(searchDiscovery({ q: 'q' })).rejects.toBeInstanceOf(TypeError);
    });
  }

  it('a section whose entries have no items throws a TypeError (well-formed JSON, unguarded cast)', async () => {
    const raw = {
      query: 'q',
      query_norm: 'q',
      results: [],
      sections: [{ kind: 'artist', has_more: false }],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 0,
      offset: 0,
      has_more: false,
    };
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    await expect(searchDiscovery({ q: 'q' })).rejects.toBeInstanceOf(TypeError);
  });

  it('a 204 landing where a DiscoverySearchResponse was expected throws a TypeError (unguarded cast dereferences undefined)', async () => {
    __http.reply('GET /v1/discovery/search', { status: 204 });

    await expect(searchDiscovery({ q: 'q' })).rejects.toBeInstanceOf(TypeError);
  });
});

describe('Table: suggestDiscovery query-string guard', () => {
  it('sends only q when limit is omitted', async () => {
    __http.reply('GET /v1/discovery/suggest', { status: 200, json: { suggestions: [] } });

    await suggestDiscovery({ q: 'rad' });

    const qp = new URLSearchParams(__http.last().query);
    expect(qp.get('q')).toBe('rad');
    expect(qp.has('limit')).toBe(false);
    expect(questionMarkCount(__http.last().url)).toBe(1);
  });

  it('sends limit when provided, including the boundary 0', async () => {
    __http.reply('GET /v1/discovery/suggest', { status: 200, json: { suggestions: [] } });

    await suggestDiscovery({ q: 'rad', limit: 0 });

    const qp = new URLSearchParams(__http.last().query);
    expect(qp.get('limit')).toBe('0');
  });
});

describe('Table: listSearchHistory query-string guard', () => {
  it('with a limit produces /v1/discovery/search-history?limit=10', async () => {
    __http.reply('GET /v1/discovery/search-history', {
      status: 200,
      json: { items: [], total: 0 },
    });

    await listSearchHistory({ limit: 10 });

    expect(__http.last().url).toBe(`${apiBase}/v1/discovery/search-history?limit=10`);
  });

  it(
    'with no argument at all produces no trailing "?" -- reachable through the exported ' +
      'signature though its only current caller always passes { limit: 10 }',
    async () => {
      __http.reply('GET /v1/discovery/search-history', {
        status: 200,
        json: { items: [], total: 0 },
      });

      await listSearchHistory();

      expect(__http.last().url).toBe(`${apiBase}/v1/discovery/search-history`);
    },
  );

  it('with an explicit empty object also produces no trailing "?"', async () => {
    __http.reply('GET /v1/discovery/search-history', {
      status: 200,
      json: { items: [], total: 0 },
    });

    await listSearchHistory({});

    expect(__http.last().url).toBe(`${apiBase}/v1/discovery/search-history`);
  });
});

describe('clearSearchHistory', () => {
  it('sends a DELETE and resolves to undefined via the 204 short-circuit', async () => {
    __http.reply('DELETE /v1/discovery/search-history', { status: 204 });

    await expect(clearSearchHistory()).resolves.toBeUndefined();
    expect(__http.last().method).toBe('DELETE');
    expect(__http.last().path).toBe('/v1/discovery/search-history');
  });
});

describe('regression: non-2xx from any discovery endpoint surfaces as ApiError, not swallowed', () => {
  it('searchDiscovery rejects with ApiError(500) rather than resolving with a partial shape', async () => {
    __http.reply('GET /v1/discovery/search', { status: 500, json: { message: 'boom' } });

    await expect(searchDiscovery({ q: 'q' })).rejects.toBeInstanceOf(ApiError);
  });
});
