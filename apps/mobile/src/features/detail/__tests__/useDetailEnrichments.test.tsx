import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { supabase } from '@shared/auth/supabaseClient';

import { useDetailEnrichments } from '../hooks/useDetailEnrichments';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function freshClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

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

// The payload each enricher answers with for an entity it found nothing for: the
// full DTO flagged has_content:false, which is what the api-client contract-parses.
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
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  // MusicBrainz is enabled for artists too; keep it a clean empty result so the
  // scenario under test is purely about the Last.fm provider.
  __http.reply('GET /v1/discovery/enrichment', { status: 200, json: emptyEnrichment });
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
});

afterEach(() => {
  warnSpy.mockRestore();
});

describe('useDetailEnrichments: a failed provider fetch vs a genuinely-empty one', () => {
  it('flags the Last.fm error while still leaving its data null, distinguishing it from no content', async () => {
    __http.fail('GET /v1/discovery/enrichment/lastfm');
    const queryClient = freshClient();

    const { result } = renderHook(() => useDetailEnrichments(artistResult()), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.errors.lastfm).toBe(true));
    // Data is null on failure — same as an empty result — so the error flag is
    // the only signal a consumer has to tell a fetch failure apart.
    expect(result.current.lastfm).toBeNull();
  });

  it('does not flag an error when Last.fm genuinely has no content for this artist', async () => {
    __http.reply('GET /v1/discovery/enrichment/lastfm', {
      status: 200,
      json: emptyLastFm,
    });
    const queryClient = freshClient();

    const { result } = renderHook(() => useDetailEnrichments(artistResult()), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.lastfm).toBeNull());
    // Same null payload as the failure case, but the error flag stays false —
    // this is the distinction the bug was collapsing to a single null.
    expect(result.current.errors.lastfm).toBe(false);
  });
});

describe('useDetailEnrichments: naming the entity and provider behind a failed fetch', () => {
  it('logs the provider, the entity and the reason when Last.fm cannot be reached', async () => {
    __http.fail('GET /v1/discovery/enrichment/lastfm');
    const queryClient = freshClient();

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
    const queryClient = freshClient();

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
