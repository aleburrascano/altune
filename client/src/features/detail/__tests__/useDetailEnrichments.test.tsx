import type { QueryClient } from '@tanstack/react-query';

import { renderHook, waitFor } from '@testing-library/react-native';
import { supabase } from '@shared/auth/supabaseClient';
import { recordEvent } from '@shared/telemetry/recordEvent';
import type { DiscoveryResult } from '@shared/apiClient/discovery';

import { _resetDetailHealthForTest } from '@/features/detail/domain/detailHealth';
import {
  useEnrichment,
  useDeezerEnrichment,
  useLastFmEnrichment,
  useDetailEnrichments,
} from '@/features/detail/hooks/useDetailEnrichments';
import {
  createTestQueryClient,
  createWrapper,
  mockSupabaseSession,
} from '@/features/detail/__tests__/support/queryHarness';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

describe('useEnrichment', () => {
  describe('aborting on unmount', () => {
    let client: QueryClient;

    let wrapper: ReturnType<typeof createWrapper>;

    beforeEach(() => {
      (supabase.auth.getSession as jest.Mock).mockResolvedValue(mockSupabaseSession());
      _resetDetailHealthForTest();
      (recordEvent as jest.Mock).mockReset().mockResolvedValue(undefined);
      client = createTestQueryClient();
      wrapper = createWrapper(client);
    });

    afterEach(() => client.clear());

    const album = { kind: 'album', title: 'OK Computer', subtitle: 'Radiohead' } as const;

    describe.each([
      ['useEnrichment', 'GET /v1/discovery/enrichment', (): void => void useEnrichment(album)],
    ] as const)('%s, left before its request resolves', (_name, spec, useHook) => {
      it('aborts the in-flight request when the screen unmounts', async () => {
        __http.hang(spec);
        const { unmount } = renderHook(useHook, { wrapper });
        await waitFor(() => expect(__http.last()).toBeDefined());
        const { signal } = __http.last() as { signal: AbortSignal };
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

    beforeEach(() => {
      (supabase.auth.getSession as jest.Mock).mockResolvedValue(mockSupabaseSession());
      _resetDetailHealthForTest();
      (recordEvent as jest.Mock).mockReset().mockResolvedValue(undefined);
      client = createTestQueryClient();
      wrapper = createWrapper(client);
    });

    afterEach(() => client.clear());

    const album = { kind: 'album', title: 'OK Computer', subtitle: 'Radiohead' } as const;

    describe.each([
      [
        'useDeezerEnrichment',
        'GET /v1/discovery/enrichment/deezer',
        (): void => void useDeezerEnrichment(album),
      ],
    ] as const)('%s, left before its request resolves', (_name, spec, useHook) => {
      it('aborts the in-flight request when the screen unmounts', async () => {
        __http.hang(spec);
        const { unmount } = renderHook(useHook, { wrapper });
        await waitFor(() => expect(__http.last()).toBeDefined());
        const { signal } = __http.last() as { signal: AbortSignal };
        expect(signal.aborted).toBe(false);

        unmount();

        await waitFor(() => expect(signal.aborted).toBe(true));
      });
    });
  });
});

describe('useLastFmEnrichment', () => {
  describe('aborting on unmount', () => {
    let client: QueryClient;

    let wrapper: ReturnType<typeof createWrapper>;

    beforeEach(() => {
      (supabase.auth.getSession as jest.Mock).mockResolvedValue(mockSupabaseSession());
      _resetDetailHealthForTest();
      (recordEvent as jest.Mock).mockReset().mockResolvedValue(undefined);
      client = createTestQueryClient();
      wrapper = createWrapper(client);
    });

    afterEach(() => client.clear());

    const album = { kind: 'album', title: 'OK Computer', subtitle: 'Radiohead' } as const;

    describe.each([
      [
        'useLastFmEnrichment',
        'GET /v1/discovery/enrichment/lastfm',
        (): void => void useLastFmEnrichment(album),
      ],
    ] as const)('%s, left before its request resolves', (_name, spec, useHook) => {
      it('aborts the in-flight request when the screen unmounts', async () => {
        __http.hang(spec);
        const { unmount } = renderHook(useHook, { wrapper });
        await waitFor(() => expect(__http.last()).toBeDefined());
        const { signal } = __http.last() as { signal: AbortSignal };
        expect(signal.aborted).toBe(false);

        unmount();

        await waitFor(() => expect(signal.aborted).toBe(true));
      });
    });
  });
});

describe('useDetailEnrichments', () => {
  function artistResult(): DiscoveryResult {
    return {
      kind: 'artist',
      title: 'Radiohead',
      subtitle: null,
      image_url: null,
      confidence: 'high',
      sources: [],
      extras: {},
    };
  }

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

  const emptyLastFm = {
    has_content: false,
    mbid: '',
    listeners: 0,
    playcount: 0,
    tags: [],
    bio: '',
    similar: [],
    duration: 0,
    album: '',
  };

  let warnSpy: jest.SpyInstance;

  beforeEach(() => {
    (supabase.auth.getSession as jest.Mock).mockResolvedValue(mockSupabaseSession());
    __http.reply('GET /v1/discovery/enrichment', { status: 200, json: emptyEnrichment });
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
  });

  afterEach(() => {
    warnSpy.mockRestore();
  });

  describe('useDetailEnrichments: a failed provider fetch vs a genuinely-empty one', () => {
    it('flags the Last.fm error while still leaving its data null, distinguishing it from no content', async () => {
      __http.fail('GET /v1/discovery/enrichment/lastfm');
      const queryClient = createTestQueryClient();

      const { result } = renderHook(() => useDetailEnrichments(artistResult()), {
        wrapper: createWrapper(queryClient),
      });

      await waitFor(() => expect(result.current.errors.lastfm).toBe(true));
      expect(result.current.lastfm).toBeNull();
    });

    it('does not flag an error when Last.fm genuinely has no content for this artist', async () => {
      __http.reply('GET /v1/discovery/enrichment/lastfm', {
        status: 200,
        json: emptyLastFm,
      });
      const queryClient = createTestQueryClient();

      const { result } = renderHook(() => useDetailEnrichments(artistResult()), {
        wrapper: createWrapper(queryClient),
      });

      await waitFor(() => expect(result.current.lastfm).toBeNull());
      expect(result.current.errors.lastfm).toBe(false);
    });
  });

  describe('useDetailEnrichments: naming the entity and provider behind a failed fetch', () => {
    it('logs the provider, the entity and the reason when Last.fm cannot be reached', async () => {
      __http.fail('GET /v1/discovery/enrichment/lastfm');
      const queryClient = createTestQueryClient();

      const { result } = renderHook(() => useDetailEnrichments(artistResult()), {
        wrapper: createWrapper(queryClient),
      });

      await waitFor(() => expect(result.current.errors.lastfm).toBe(true));

      expect(warnSpy).toHaveBeenCalledWith(
        '[detail] enrichment fetch failed',
        expect.objectContaining({
          provider: 'lastfm',
          kind: 'artist',
          title: 'Radiohead',
          error: expect.stringContaining('unreachable'),
        }),
      );
    });

    it('stays silent when Last.fm genuinely has no content for this artist', async () => {
      __http.reply('GET /v1/discovery/enrichment/lastfm', {
        status: 200,
        json: emptyLastFm,
      });
      const queryClient = createTestQueryClient();

      const { result } = renderHook(() => useDetailEnrichments(artistResult()), {
        wrapper: createWrapper(queryClient),
      });

      await waitFor(() => expect(result.current.lastfm).toBeNull());

      expect(warnSpy).not.toHaveBeenCalledWith(
        '[detail] enrichment fetch failed',
        expect.anything(),
      );
    });
  });
});
