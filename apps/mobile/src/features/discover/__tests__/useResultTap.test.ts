import { act, renderHook } from '@testing-library/react-native';
import { Keyboard } from 'react-native';

import { useResultTap } from '../hooks/useResultTap';
import { stashHandoffForDetail } from '../handoff';
import { resultFixture } from './fixtures';

import type { DiscoveryResult, DiscoverySearchResponse } from '@shared/api-client/discovery';

const mockMutate = jest.fn();
const mockPush = jest.fn();
const mockUseFocusEffect = jest.fn();

jest.mock('@shared/telemetry/useRecordEvent', () => ({
  useRecordEvent: () => ({ mutate: mockMutate }),
}));
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush }),
  useFocusEffect: (effect: () => void) => mockUseFocusEffect(effect),
}));
jest.mock('../handoff', () => ({
  stashHandoffForDetail: jest.fn(() => '/discover/detail'),
}));

const mockStash = stashHandoffForDetail as jest.Mock;

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

beforeEach(() => {
  jest.clearAllMocks();
});

describe('useResultTap records result_clicked and hands off to the detail screen', () => {
  it('uses the global index, search identity and signature from the response', () => {
    const dismiss = jest.spyOn(Keyboard, 'dismiss');
    const data = responseFixture();
    const tapped = data.results[1]!;
    const { result } = renderHook(() => useResultTap(data));

    result.current(tapped, 0);

    expect(dismiss).toHaveBeenCalled();
    expect(mockMutate).toHaveBeenCalledWith({
      type: 'result_clicked',
      search_id: 'search-1',
      payload: {
        kind: 'track',
        title: 'second',
        subtitle: null,
        position: 1,
        confidence: 'high',
        provider: 'spotify',
        result_signature: 'sig',
      },
    });
    expect(mockStash).toHaveBeenCalledWith(tapped, 'search-1');
    expect(mockPush).toHaveBeenCalledWith('/discover/detail');
  });

  it('logs the global rank for a blended-view tap whose object is a parsed copy, not a results[] reference', () => {
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
    // top_result / sections[].items are parsed independently from results[], so the same
    // logical entry arrives as a structurally equal but distinct object.
    const copy = (r: DiscoveryResult): DiscoveryResult => structuredClone(r);
    const data = responseFixture({
      results,
      top_result: copy(results[0]!),
      sections: [{ kind: 'track', items: [copy(results[1]!), copy(results[2]!)], has_more: false }],
    });
    const { result } = renderHook(() => useResultTap(data));
    const onFocus = () => act(() => (mockUseFocusEffect.mock.calls.at(-1)![0] as () => void)());

    result.current(data.sections[0]!.items[1]!, 1);
    onFocus();
    result.current(data.top_result!, 0);

    expect(
      mockMutate.mock.calls.map(([e]) => (e as { payload: { position: number } }).payload.position),
    ).toEqual([2, 0]);
  });

  it('matches by result signature when the tapped copy carries no source identity', () => {
    const results = [
      resultFixture({ title: 'a', result_signature: 'track|a|', sources: [] }),
      resultFixture({ title: 'b', result_signature: 'track|b|', sources: [] }),
    ];
    const data = responseFixture({ results });
    const { result } = renderHook(() => useResultTap(data));

    result.current(structuredClone(results[1]!), 0);

    expect(mockMutate.mock.calls[0]![0].payload.position).toBe(1);
  });

  it('falls back to the passed position, and omits a missing signature', () => {
    const orphan = resultFixture({ result_signature: undefined, sources: [] });
    const { result } = renderHook(() => useResultTap(undefined));

    result.current(orphan, 4);

    expect(mockMutate).toHaveBeenCalledWith({
      type: 'result_clicked',
      search_id: undefined,
      payload: {
        kind: 'track',
        title: 'The Title',
        subtitle: null,
        position: 4,
        confidence: 'high',
        provider: null,
      },
    });
  });

  it('ignores a second tap while the first navigation is pending, until the screen refocuses', () => {
    const data = responseFixture();
    const [first, second] = data.results;
    const { result } = renderHook(() => useResultTap(data));

    result.current(first!, 0);
    result.current(second!, 1);

    expect(mockPush).toHaveBeenCalledTimes(1);
    expect(mockMutate).toHaveBeenCalledTimes(1);
    expect(mockStash).toHaveBeenCalledTimes(1);
    expect(mockStash).toHaveBeenCalledWith(first, 'search-1');

    const onFocus = mockUseFocusEffect.mock.calls.at(-1)![0] as () => void;
    act(() => onFocus());
    result.current(second!, 1);

    expect(mockPush).toHaveBeenCalledTimes(2);
    expect(mockStash).toHaveBeenLastCalledWith(second, 'search-1');
  });
});
