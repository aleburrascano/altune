import type { QueryClient } from '@tanstack/react-query';

import { renderHook, waitFor } from '@testing-library/react-native';
import type { DiscoveryResult } from '@shared/catalog';
import { _resetDetailHealthForTest, httpDetailPort, useDetailEnrichments } from '@features/detail';
import type { DetailPort } from '@features/detail';
import type { DetailWarning } from '@testing/fakes/detail/inMemoryDetailPort';
import {
  createPortWrapper,
  createTestQueryClient,
  createWrapper,
  signInTestUser,
} from '@testing/queryHarness';

const { __http } = require('@testing/doubles/fetch.js');

function resultOf(
  kind: 'album' | 'artist',
  title: string,
  subtitle: string | null,
): DiscoveryResult {
  return { kind, title, subtitle, image_url: null, confidence: 'high', sources: [], extras: {} };
}

type AbortableRequest = { path: string; signal: AbortSignal };

function requestFor(spec: string): AbortableRequest {
  const path = spec.slice(spec.indexOf(' ') + 1);
  const requests = __http.requests as AbortableRequest[];
  return requests.find((request) => request.path === path)!;
}

describe('useEnrichment', () => {
  describe('aborting on unmount', () => {
    let client: QueryClient;

    let wrapper: ReturnType<typeof createWrapper>;

    beforeEach(async () => {
      await signInTestUser();
      _resetDetailHealthForTest();
      client = createTestQueryClient();
      wrapper = createWrapper(client);
    });

    afterEach(() => client.clear());

    const album = resultOf('album', 'OK Computer', 'Radiohead');

    describe.each([
      [
        'useEnrichment',
        'GET /v1/discovery/enrichment',
        (): void => void useDetailEnrichments(album),
      ],
    ] as const)('%s, left before its request resolves', (_name, spec, useHook) => {
      it('aborts the in-flight request when the screen unmounts', async () => {
        __http.hang(spec);
        const { unmount } = renderHook(useHook, { wrapper });
        await waitFor(() => expect(__http.countFor(spec)).toBeGreaterThan(0));
        const { signal } = requestFor(spec);
        expect(signal.aborted).toBe(false);

        unmount();

        await waitFor(() => expect(signal.aborted).toBe(true));
      });
    });
  });
});

describe('useDeezerEnrichment', () => {
  describe('aborting on unmount', () => {
    let client: QueryClient;

    let wrapper: ReturnType<typeof createWrapper>;

    beforeEach(async () => {
      await signInTestUser();
      _resetDetailHealthForTest();
      client = createTestQueryClient();
      wrapper = createWrapper(client);
    });

    afterEach(() => client.clear());

    const album = resultOf('album', 'OK Computer', 'Radiohead');

    describe.each([
      [
        'useDeezerEnrichment',
        'GET /v1/discovery/enrichment/deezer',
        (): void => void useDetailEnrichments(album),
      ],
    ] as const)('%s, left before its request resolves', (_name, spec, useHook) => {
      it('aborts the in-flight request when the screen unmounts', async () => {
        __http.hang(spec);
        const { unmount } = renderHook(useHook, { wrapper });
        await waitFor(() => expect(__http.countFor(spec)).toBeGreaterThan(0));
        const { signal } = requestFor(spec);
        expect(signal.aborted).toBe(false);

        unmount();

        await waitFor(() => expect(signal.aborted).toBe(true));
      });
    });
  });
});

describe('useDetailEnrichments', () => {
  const DEEZER = 'GET /v1/discovery/enrichment/deezer';

  const emptyEnrichment = {
    has_content: false,
    mbid: '',
    genres: [],
    year: 0,
    rating: 0,
    rating_votes: 0,
    primary_type: '',
    secondary_types: [],
    external_ids: {},
    artwork_url: '',
  };

  const emptyDeezer = {
    has_content: false,
    bpm: 0,
    gain: 0,
    explicit: false,
    label: '',
    genres: [],
    upc: '',
    record_type: '',
  };

  const album = resultOf('album', 'OK Computer', 'Radiohead');

  let warnings: DetailWarning[];

  function recordingWrapper(queryClient: QueryClient) {
    const detail: DetailPort = {
      ...httpDetailPort,
      warn: (message, context) => {
        warnings.push([message, context]);
      },
    };
    return createPortWrapper(queryClient, { detail });
  }

  beforeEach(async () => {
    await signInTestUser();
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: emptyEnrichment });
    warnings = [];
  });

  describe('useDetailEnrichments: asking only the providers a page shows', () => {
    it('asks no enrichment provider on an artist page, which shows nothing from them', async () => {
      const queryClient = createTestQueryClient();

      const { result } = renderHook(
        () => useDetailEnrichments(resultOf('artist', 'Radiohead', null)),
        { wrapper: recordingWrapper(queryClient) },
      );

      await waitFor(() => expect(result.current.deezer).toBeNull());
      expect(__http.requests).toHaveLength(0);
    });
  });

  describe('useDetailEnrichments: a failed provider fetch vs a genuinely-empty one', () => {
    it('flags the Deezer error while still leaving its data null, distinguishing it from no content', async () => {
      __http.fail(DEEZER);
      const queryClient = createTestQueryClient();

      const { result } = renderHook(() => useDetailEnrichments(album), {
        wrapper: recordingWrapper(queryClient),
      });

      await waitFor(() => expect(result.current.errors.deezer).toBe(true));
      expect(result.current.deezer).toBeNull();
    });

    it('does not flag an error when Deezer genuinely has no content for this album', async () => {
      __http.reply(DEEZER, { status: 200, json: emptyDeezer });
      const queryClient = createTestQueryClient();

      const { result } = renderHook(() => useDetailEnrichments(album), {
        wrapper: recordingWrapper(queryClient),
      });

      await waitFor(() => expect(__http.countFor(DEEZER)).toBe(1));
      await waitFor(() => expect(result.current.deezer).toBeNull());
      expect(result.current.errors.deezer).toBe(false);
    });
  });

  describe('useDetailEnrichments: naming the entity and provider behind a failed fetch', () => {
    it('logs the provider, the entity and the reason when Deezer cannot be reached', async () => {
      __http.fail(DEEZER);
      const queryClient = createTestQueryClient();

      const { result } = renderHook(() => useDetailEnrichments(album), {
        wrapper: recordingWrapper(queryClient),
      });

      await waitFor(() => expect(result.current.errors.deezer).toBe(true));

      expect(warnings).toContainEqual([
        '[detail] enrichment fetch failed',
        expect.objectContaining({
          provider: 'deezer',
          kind: 'album',
          title: 'OK Computer',
          error: expect.stringContaining('unreachable'),
        }),
      ]);
    });

    it('stays silent when Deezer genuinely has no content for this album', async () => {
      __http.reply(DEEZER, { status: 200, json: emptyDeezer });
      const queryClient = createTestQueryClient();

      renderHook(() => useDetailEnrichments(album), {
        wrapper: recordingWrapper(queryClient),
      });

      await waitFor(() => expect(__http.countFor(DEEZER)).toBe(1));
      await waitFor(() => expect(__http.countFor('GET /v1/discovery/enrichment')).toBe(1));

      expect(warnings).not.toContainEqual(['[detail] enrichment fetch failed', expect.anything()]);
    });
  });
});
