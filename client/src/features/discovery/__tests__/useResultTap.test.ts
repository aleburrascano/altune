import { QueryClient } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { useResultTap } from '@features/discovery';
import { resultFixture } from '@features/discovery/__tests__/fixtures';
import { discoverEnv, type TestEnv } from '@features/discovery/__tests__/discoverEnv';
import { readDetailHandoff } from '@shared/navigation';
import { runSignOutCleanups } from '@shared/session';
import { makeWrapper } from '@testing/makeWrapper';
import { signInTestUser } from '@testing/signInTestUser';
import type { DiscoveryResult, DiscoverySearchResponse } from '@shared/catalog';

const { __http } = require('@testing/doubles/fetch.js');

const EVENTS = 'POST /v1/discovery/events';
const EVENTS_PATH = '/v1/discovery/events';
const TWO_EVENTS = 2;
const TWO_PUSHES = 2;
const TOP_RESULT_RANK = 2;

interface RecordedRequest {
  path: string;
  body: string;
}

interface RecordedEvent {
  type: string;
  search_id?: string;
  payload: { position: number } & Record<string, unknown>;
}

function sentEvents(): RecordedEvent[] {
  return (__http.requests as RecordedRequest[])
    .filter((request) => request.path === EVENTS_PATH)
    .map((request) => JSON.parse(request.body) as RecordedEvent);
}

let fake: TestEnv;
let queryClient: QueryClient;
let wrapper: ReturnType<typeof makeWrapper>;

interface PushedDetailHref {
  pathname: string;
  params: { handoff: string };
}

function pushedHref(call: number): PushedDetailHref {
  return fake.navigator.push.mock.calls[call]![0] as PushedDetailHref;
}

function pushedHandoff(call: number) {
  return readDetailHandoff(pushedHref(call).params.handoff);
}

function responseFixture(
  overrides: Partial<DiscoverySearchResponse> = {},
): DiscoverySearchResponse {
  return {
    query: 'Radiohead',
    query_norm: 'radiohead',
    search_id: 'search-1',
    results: [resultFixture({ title: 'first' }), resultFixture({ title: 'second' })],
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 2,
    offset: 0,
    has_more: false,
    ...overrides,
  };
}

beforeEach(async () => {
  fake = discoverEnv();
  runSignOutCleanups();
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  wrapper = makeWrapper(queryClient);
  await signInTestUser();
  __http.reply(EVENTS, { status: 204 });
});

afterEach(() => {
  __http.reset();
  queryClient.clear();
});

describe('useResultTap records result_clicked and hands off to the detail screen', () => {
  it('uses the global index, search identity and signature from the response', async () => {
    const data = responseFixture();
    const tapped = data.results[1]!;
    const { result } = renderHook(() => useResultTap(data, fake.env), { wrapper });

    result.current(tapped, 0);

    expect(fake.dismissKeyboard).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(sentEvents()).toHaveLength(1));
    expect(sentEvents()[0]).toEqual({
      type: 'result_clicked',
      search_id: 'search-1',
      payload: {
        session_id: expect.any(String),
        kind: 'track',
        title: 'second',
        subtitle: null,
        position: 1,
        confidence: 'high',
        provider: 'spotify',
        result_signature: 'sig',
      },
    });
    expect(fake.navigator.push).toHaveBeenCalledTimes(1);
    expect(pushedHref(0).pathname).toBe('/discover/detail');
    expect(pushedHandoff(0)?.result).toBe(tapped);
    expect(pushedHandoff(0)?.searchId).toBe('search-1');
  });

  it('logs the global rank for a blended-view tap whose object is a parsed copy, not a results[] reference', async () => {
    const results = [
      resultFixture({
        kind: 'artist',
        title: 'Radiohead',
        result_signature: 'artist|radiohead|',
        sources: [{ provider: 'spotify', external_id: 'art-1', url: 'https://x' }],
      }),
      resultFixture({
        title: 'Creep',
        result_signature: 'track|creep|radiohead',
        sources: [{ provider: 'spotify', external_id: 'trk-1', url: 'https://x' }],
      }),
      resultFixture({
        title: 'Karma Police',
        result_signature: 'track|karma police|radiohead',
        sources: [{ provider: 'deezer', external_id: 'trk-2', url: 'https://x' }],
      }),
    ];
    const copy = (r: DiscoveryResult): DiscoveryResult => structuredClone(r);
    const data = responseFixture({
      results,
      top_result: copy(results[0]!),
      sections: [{ kind: 'track', items: [copy(results[1]!), copy(results[2]!)], has_more: false }],
    });
    const { result } = renderHook(() => useResultTap(data, fake.env), { wrapper });

    result.current(data.sections[0]!.items[1]!, 1);
    act(() => fake.focus());
    result.current(data.top_result!, 0);

    await waitFor(() => expect(sentEvents()).toHaveLength(TWO_EVENTS));
    expect(sentEvents().map((e) => e.payload.position)).toEqual([TOP_RESULT_RANK, 0]);
  });

  it('matches by result signature when the tapped copy carries no source identity', async () => {
    const results = [
      resultFixture({ title: 'a', result_signature: 'track|a|', sources: [] }),
      resultFixture({ title: 'b', result_signature: 'track|b|', sources: [] }),
    ];
    const data = responseFixture({ results });
    const { result } = renderHook(() => useResultTap(data, fake.env), { wrapper });

    result.current(structuredClone(results[1]!), 0);

    await waitFor(() => expect(sentEvents()).toHaveLength(1));
    expect(sentEvents()[0]!.payload.position).toBe(1);
  });

  it('falls back to the passed position, and omits a missing signature', async () => {
    const orphan = resultFixture({ result_signature: undefined, sources: [] });
    const { result } = renderHook(() => useResultTap(undefined, fake.env), { wrapper });

    result.current(orphan, 4);

    await waitFor(() => expect(sentEvents()).toHaveLength(1));
    expect(sentEvents()[0]).toEqual({
      type: 'result_clicked',
      payload: {
        session_id: expect.any(String),
        kind: 'track',
        title: 'The Title',
        subtitle: null,
        position: 4,
        confidence: 'high',
        provider: null,
      },
    });
  });

  it('ignores a second tap while the first navigation is pending, until the screen refocuses', async () => {
    const data = responseFixture();
    const [first, second] = data.results;
    const { result } = renderHook(() => useResultTap(data, fake.env), { wrapper });

    result.current(first!, 0);
    result.current(second!, 1);

    expect(fake.navigator.push).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(sentEvents()).toHaveLength(1));
    expect(pushedHandoff(0)?.result).toBe(first);

    act(() => fake.focus());
    result.current(second!, 1);

    expect(fake.navigator.push).toHaveBeenCalledTimes(TWO_PUSHES);
    expect(pushedHandoff(1)?.result).toBe(second);
    expect(pushedHandoff(1)?.searchId).toBe('search-1');
  });
});
