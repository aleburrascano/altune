import {
  searchDiscovery,
  suggestDiscovery,
  listSearchHistory,
  clearSearchHistory,
  parseDiscoverySearchResponse,
} from '../discovery';
import type { DiscoveryKind, DiscoveryResult, DiscoverySearchResponse } from '../discovery';
import { apiBase, ApiError, ContractError } from '../index';
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

describe('AbortSignal threading through suggestDiscovery (a keystroke supersedes the request before it, so the thread is what lets react-query drop it)', () => {
  it('a pre-aborted caller-supplied signal cancels the request before it ever resolves', async () => {
    __http.reply('GET /v1/discovery/suggest', { status: 200, json: { suggestions: [] } });
    const controller = new AbortController();
    controller.abort();

    await expect(suggestDiscovery({ q: 'rad' }, controller.signal)).rejects.toMatchObject({
      name: 'AbortError',
    });
  });

  it('aborting the caller-supplied signal mid-flight cancels a hung request', async () => {
    __http.hang('GET /v1/discovery/suggest');
    const controller = new AbortController();

    const pending = suggestDiscovery({ q: 'rad' }, controller.signal);
    controller.abort();

    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('with no caller-supplied signal, the request is not pre-aborted and completes normally', async () => {
    __http.reply('GET /v1/discovery/suggest', { status: 200, json: { suggestions: [] } });

    await suggestDiscovery({ q: 'rad' });

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

describe('Adversarial: malformed/off-contract discovery payloads fail as a typed ContractError', () => {
  it('a DiscoveryResult missing its required sources array is a ContractError, not a pass-through', async () => {
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

    await expect(searchDiscovery({ q: 'q' })).rejects.toBeInstanceOf(ContractError);
  });

  it('a null response body is a ContractError, not a TypeError that escapes the boundary', async () => {
    __http.reply('GET /v1/discovery/search', { status: 200, json: null });

    await expect(searchDiscovery({ q: 'q' })).rejects.toBeInstanceOf(ContractError);
  });

  const wrongTypeResultsCases: [string, unknown][] = [
    ['an object', { not: 'an array' }],
    ['a string', 'nope'],
    ['a number', 42],
  ];

  for (const [label, value] of wrongTypeResultsCases) {
    it(`results as ${label} is a ContractError`, async () => {
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

      await expect(searchDiscovery({ q: 'q' })).rejects.toBeInstanceOf(ContractError);
    });
  }

  it('a section whose entries have no items is a ContractError', async () => {
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

    await expect(searchDiscovery({ q: 'q' })).rejects.toBeInstanceOf(ContractError);
  });

  it('a 204 landing where a DiscoverySearchResponse was expected is a ContractError', async () => {
    __http.reply('GET /v1/discovery/search', { status: 204 });

    await expect(searchDiscovery({ q: 'q' })).rejects.toBeInstanceOf(ContractError);
  });
});

describe('Off-contract union values fail at the boundary rather than flowing on (the highlighted gap)', () => {
  function responseWith(result: Record<string, unknown>): unknown {
    return {
      query: 'q',
      query_norm: 'q',
      results: [result],
      sections: [],
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 1,
      offset: 0,
      has_more: false,
    };
  }

  function baseResult(): Record<string, unknown> {
    return {
      kind: 'track',
      title: 'Weird Fishes',
      subtitle: null,
      image_url: null,
      confidence: 'high',
      sources: [],
      extras: {},
    };
  }

  it("a confidence outside 'high'|'medium'|'low' is a ContractError, not passed through as a bad union", async () => {
    __http.reply('GET /v1/discovery/search', {
      status: 200,
      json: responseWith({ ...baseResult(), confidence: 'extreme' }),
    });

    await expect(searchDiscovery({ q: 'q' })).rejects.toBeInstanceOf(ContractError);
  });

  it("a kind outside 'artist'|'album'|'track' is a ContractError", async () => {
    __http.reply('GET /v1/discovery/search', {
      status: 200,
      json: responseWith({ ...baseResult(), kind: 'playlist' }),
    });

    await expect(searchDiscovery({ q: 'q' })).rejects.toBeInstanceOf(ContractError);
  });

  it('a provider status outside the declared set is a ContractError', async () => {
    const raw = {
      query: 'q',
      query_norm: 'q',
      results: [],
      sections: [],
      providers: [{ provider: 'musicbrainz', status: 'exploded', result_count: 0, latency_ms: 5 }],
      partial: false,
      cache: { hit: false, fetched_at: null },
      total: 0,
      offset: 0,
      has_more: false,
    };
    __http.reply('GET /v1/discovery/search', { status: 200, json: raw });

    await expect(searchDiscovery({ q: 'q' })).rejects.toBeInstanceOf(ContractError);
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

describe('Adversarial: malformed suggest/search-history payloads fail as a typed ContractError', () => {
  it('suggestDiscovery parses a well-formed response through to typed suggestions', async () => {
    const json = { suggestions: [{ text: 'radiohead', kind: 'artist', popularity: 7 }] };
    __http.reply('GET /v1/discovery/suggest', { status: 200, json });

    await expect(suggestDiscovery({ q: 'rad' })).resolves.toEqual(json);
  });

  const malformedSuggest: [string, unknown][] = [
    ['a null body', null],
    ['a JSON string body (proxy error text)', '<html>502 Bad Gateway</html>'],
    ['suggestions as a non-array', { suggestions: { text: 'radiohead' } }],
    ['suggestions missing entirely (renamed envelope)', { items: [] }],
    ['a suggestion missing text', { suggestions: [{ kind: 'artist', popularity: 1 }] }],
    ['a suggestion missing kind', { suggestions: [{ text: 'radiohead', popularity: 1 }] }],
    [
      'a suggestion with a non-numeric popularity',
      { suggestions: [{ text: 'radiohead', kind: 'artist', popularity: 'high' }] },
    ],
  ];

  it.each(malformedSuggest)('suggestDiscovery rejects %s', async (_label, json) => {
    __http.reply('GET /v1/discovery/suggest', { status: 200, json });

    await expect(suggestDiscovery({ q: 'rad' })).rejects.toBeInstanceOf(ContractError);
  });

  it('listSearchHistory parses a well-formed response through to typed items', async () => {
    const json = {
      items: [
        { query: 'Radiohead', query_norm: 'radiohead', executed_at: '2026-01-01T00:00:00.000Z' },
      ],
      total: 1,
    };
    __http.reply('GET /v1/discovery/search-history', { status: 200, json });

    await expect(listSearchHistory({ limit: 10 })).resolves.toEqual(json);
  });

  const malformedHistory: [string, unknown][] = [
    ['a null body', null],
    ['a JSON string body (proxy error text)', '<html>502 Bad Gateway</html>'],
    ['items as a non-array', { items: 'nope', total: 0 }],
    ['total missing', { items: [] }],
    [
      'an item missing query',
      { items: [{ query_norm: 'radiohead', executed_at: '2026-01-01T00:00:00.000Z' }], total: 1 },
    ],
    [
      'an item missing executed_at',
      { items: [{ query: 'Radiohead', query_norm: 'radiohead' }], total: 1 },
    ],
  ];

  it.each(malformedHistory)('listSearchHistory rejects %s', async (_label, json) => {
    __http.reply('GET /v1/discovery/search-history', { status: 200, json });

    await expect(listSearchHistory({ limit: 10 })).rejects.toBeInstanceOf(ContractError);
  });
});

describe('search_id', () => {
  beforeEach(() => {
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
  });

  function fullSearchResponse() {
    return {
      query: 'radiohead',
      query_norm: 'radiohead',
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

  describe('searchDiscovery sends search_id so a later page reads the held slate', () => {
    it('omits search_id when none is given', async () => {
      __http.reply('GET /v1/discovery/search', { status: 200, json: fullSearchResponse() });

      await searchDiscovery({ q: 'radiohead', offset: 5 });

      const qp = new URLSearchParams(__http.last().query);
      expect(qp.has('search_id')).toBe(false);
    });

    it('forwards a given searchId as the search_id query param', async () => {
      __http.reply('GET /v1/discovery/search', { status: 200, json: fullSearchResponse() });

      await searchDiscovery({
        q: 'radiohead',
        offset: 20,
        searchId: '9f2c1b1e-2222-4444-8888-000000000001',
      });

      const qp = new URLSearchParams(__http.last().query);
      expect(qp.get('search_id')).toBe('9f2c1b1e-2222-4444-8888-000000000001');
      expect(qp.get('offset')).toBe('20');
    });
  });
});

describe('wire parsing', () => {

  function fullResult(overrides: Record<string, unknown> = {}): Record<string, unknown> {
    return {
      kind: 'track',
      title: 'Reckoner',
      subtitle: 'Radiohead',
      image_url: 'https://img/reckoner.png',
      confidence: 'high',
      sources: [{ provider: 'musicbrainz', external_id: 'mb-1', url: 'https://mb/1' }],
      extras: {},
      result_signature: 'sig-1',
      favorite_key: 'fav-1',
      ...overrides,
    };
  }

  describe('parseDiscoverySearchResponse — the full, structured shape', () => {
    const parsed = parseDiscoverySearchResponse({
      query: 'radiohead',
      query_norm: 'radiohead',
      results: [fullResult()],
      sections: [{ kind: 'album', items: [fullResult()], has_more: true }],
      providers: [{ provider: 'musicbrainz', status: 'ok', result_count: 3, latency_ms: 12 }],
      partial: false,
      cache: { hit: true, fetched_at: '2024-01-01T00:00:00Z' },
      total: 500,
      offset: 20,
      has_more: true,
      search_id: 'srch-1',
      top_result: fullResult({ kind: 'artist', title: 'Radiohead' }),
      corrected_query: 'radiohead',
      original_query: 'radiohed',
      related: [
        { relationship: 'similar', related_to: 'Radiohead', items: [fullResult({ kind: 'artist' })] },
      ],
    });

    it('carries the structured optional fields through', () => {
      expect(parsed.search_id).toBe('srch-1');
      expect(parsed.corrected_query).toBe('radiohead');
      expect(parsed.original_query).toBe('radiohed');
      expect(parsed.top_result!.title).toBe('Radiohead');
      expect(parsed.related![0]!.relationship).toBe('similar');
    });

    it('parses the provider list, cache, and per-result signature/favorite_key', () => {
      expect(parsed.providers[0]!.status).toBe('ok');
      expect(parsed.providers[0]!.result_count).toBe(3);
      expect(parsed.cache).toEqual({ hit: true, fetched_at: '2024-01-01T00:00:00Z' });
      expect(parsed.results[0]!.result_signature).toBe('sig-1');
      expect(parsed.results[0]!.favorite_key).toBe('fav-1');
      expect(parsed.sections[0]!.items).toHaveLength(1);
    });
  });

  describe('parseDiscoverySearchResponse — legacy server omitting newer fields', () => {
    const parsed = parseDiscoverySearchResponse({
      query: 'q',
      query_norm: 'q',
      providers: [],
      partial: false,
      cache: { hit: false, fetched_at: null },
    });

    it('defaults results, sections, offset, has_more, and total (to results.length) rather than throwing', () => {
      expect(parsed.results).toEqual([]);
      expect(parsed.sections).toEqual([]);
      expect(parsed.offset).toBe(0);
      expect(parsed.has_more).toBe(false);
      expect(parsed.total).toBe(0);
    });

    it('omits the structured optional fields entirely', () => {
      expect(parsed).not.toHaveProperty('search_id');
      expect(parsed).not.toHaveProperty('top_result');
      expect(parsed).not.toHaveProperty('corrected_query');
      expect(parsed).not.toHaveProperty('original_query');
      expect(parsed).not.toHaveProperty('related');
    });
  });

  describe('parseDiscoverySearchResponse — off-contract bodies fail as a ContractError', () => {
    function base(): Record<string, unknown> {
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

    it('rejects a null body', () => {
      expect(() => parseDiscoverySearchResponse(null)).toThrow(ContractError);
    });

    it('rejects an off-contract result confidence', () => {
      expect(() =>
        parseDiscoverySearchResponse({ ...base(), results: [fullResult({ confidence: 'extreme' })] }),
      ).toThrow(ContractError);
    });

    it('rejects an off-contract result kind', () => {
      expect(() =>
        parseDiscoverySearchResponse({ ...base(), results: [fullResult({ kind: 'playlist' })] }),
      ).toThrow(ContractError);
    });

    it('rejects an off-contract provider status', () => {
      expect(() =>
        parseDiscoverySearchResponse({
          ...base(),
          providers: [{ provider: 'mb', status: 'exploded', result_count: 0, latency_ms: 1 }],
        }),
      ).toThrow(ContractError);
    });
  });
});
