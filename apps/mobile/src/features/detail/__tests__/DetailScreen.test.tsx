/**
 * DetailScreen â€” header from handoff, empty-handoff redirect, per-kind bodies,
 * and the optimistic Save action (view-result-detail slices 11-16).
 *
 * expo-image and expo-router are mocked (Artwork -> expo-image and the router
 * don't run under jest). The track body uses useSaveTrack, so track renders are
 * wrapped in a QueryClientProvider; createTrack is mocked so Save never hits
 * the network.
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import { fireEvent, render, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

import { clearDetailHandoff, setDetailHandoff } from '@shared/lib/detail-handoff';
import type { DiscoveryResult } from '../../../shared/api-client/discovery';

jest.mock('expo-image', () => ({ Image: () => null }));

const mockBack = jest.fn();
const mockRedirect = jest.fn((_props: { href: string }) => null);
jest.mock('expo-router', () => ({
  useRouter: () => ({ back: mockBack, push: jest.fn(), replace: jest.fn(), canGoBack: () => true }),
  useSegments: () => ['(tabs)', 'discover', 'detail'],
  Redirect: (props: { href: string }) => mockRedirect(props),
}));

const mockCreateTrack = jest.fn((_body: unknown) => new Promise<never>(() => {}));
jest.mock('../../../shared/api-client/tracks', () => ({
  createTrack: (body: unknown) => mockCreateTrack(body),
  getTracks: jest.fn(),
}));

const mockSearchDiscovery = jest.fn();
const mockGetAlbumTracks = jest.fn(() =>
  Promise.resolve({ items: [], provider: 'deezer', status: 'ok', latency_ms: 50 }),
);
const mockGetArtistTopTracks = jest.fn(() =>
  Promise.resolve({ items: [], provider: 'deezer', status: 'ok', latency_ms: 50 }),
);
const mockGetArtistAlbums = jest.fn(() =>
  Promise.resolve({ items: [], provider: 'deezer', status: 'ok', latency_ms: 50 }),
);
const mockGetRelatedTracks = jest.fn(() =>
  Promise.resolve({ items: [], provider: 'soundcloud', status: 'ok', latency_ms: 50 }),
);
const mockGetEnrichment = jest.fn(() =>
  Promise.resolve({
    mbid: '',
    genres: [],
    year: 0,
    rating: 0,
    rating_votes: 0,
    primary_type: '',
    secondary_types: [],
    external_ids: {},
    artwork_url: '',
  }),
);
jest.mock('../../../shared/api-client/discovery', () => ({
  searchDiscovery: (params: unknown) => mockSearchDiscovery(params),
  getAlbumTracks: () => mockGetAlbumTracks(),
  getArtistTopTracks: () => mockGetArtistTopTracks(),
  getArtistAlbums: () => mockGetArtistAlbums(),
  getRelatedTracks: () => mockGetRelatedTracks(),
  getEnrichment: () => mockGetEnrichment(),
}));

function _result(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: 'Midnight City',
    subtitle: 'M83',
    image_url: 'https://img.example/mc.jpg',
    confidence: 'high',
    sources: [],
    extras: {},
    ...overrides,
  };
}

function renderDetail(): ReturnType<typeof render> {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const { DetailScreen } = require('../ui/DetailScreen');
  const { PlaybackProvider } = require('../../playback/hooks/PlaybackProvider');
  const wrapper = ({ children }: { children: ReactNode }): ReactNode =>
    createElement(QueryClientProvider, { client: qc },
      createElement(PlaybackProvider, null, children));
  return render(createElement(DetailScreen), { wrapper });
}

afterEach(() => {
  clearDetailHandoff();
  jest.clearAllMocks();
});

describe('DetailScreen', () => {
  it('renders the header from the handoff result', () => {
    setDetailHandoff(_result());
    const { getByTestId, getByText } = renderDetail();
    expect(getByTestId('detail-header')).toBeTruthy();
    expect(getByText('Midnight City')).toBeTruthy();
    expect(mockRedirect).not.toHaveBeenCalled();
  });

  it('redirects to /discover when the handoff is empty', () => {
    clearDetailHandoff();
    renderDetail();
    expect(mockRedirect).toHaveBeenCalledWith({ href: '/discover' });
  });

  it('renders the album nav row for present album extra and omits absent keys', () => {
    // Reworked detail: duration moved onto the Play button, album/featuring became
    // tappable nav rows; the old isrc/popularity info rows are gone entirely.
    setDetailHandoff(_result({ extras: { duration_seconds: 244, album: 'After Hours' } }));
    const { getByTestId, queryByTestId } = renderDetail();
    expect(getByTestId('detail-info-album')).toBeTruthy();
    expect(queryByTestId('detail-info-isrc')).toBeNull();
    expect(queryByTestId('detail-info-popularity')).toBeNull();
  });

  it('shows the tracklist placeholder and no save button for an album', () => {
    setDetailHandoff(_result({ kind: 'album', subtitle: 'The Weeknd', extras: {} }));
    const { getByTestId, queryByTestId } = renderDetail();
    expect(getByTestId('detail-tracklist-empty')).toBeTruthy();
    expect(queryByTestId('detail-track-info')).toBeNull();
    expect(queryByTestId('detail-save')).toBeNull();
  });

  it('shows the discography placeholder and no save button for an artist', () => {
    setDetailHandoff(_result({ kind: 'artist', subtitle: null, extras: {} }));
    const { getByTestId, queryByTestId } = renderDetail();
    expect(getByTestId('detail-artist-content')).toBeTruthy();
    expect(queryByTestId('detail-save')).toBeNull();
  });

  it('saves the track with a mapped body when Save is pressed', async () => {
    setDetailHandoff(_result({ extras: { album: 'Hurry Up', duration_seconds: 244 } }));
    const { getByTestId } = renderDetail();
    fireEvent.press(getByTestId('detail-save'));
    // onMutate awaits cancelQueries, so the POST fires a microtask later.
    await waitFor(() =>
      expect(mockCreateTrack).toHaveBeenCalledWith({
        title: 'Midnight City',
        artist: 'M83',
        album: 'Hurry Up',
        duration_seconds: 244,
        artwork_url: 'https://img.example/mc.jpg',
        isrc: null,
        year: null,
        genre: null,
        album_artist: null,
        source_url: null,
      }),
    );
  });

  it('disables Save (no POST) when the track has no artist', async () => {
    setDetailHandoff(_result({ subtitle: null }));
    const { getByTestId } = renderDetail();
    fireEvent.press(getByTestId('detail-save'));
    expect(getByTestId('detail-save').props.accessibilityState?.disabled).toBe(true);
    await new Promise((r) => setTimeout(r, 0));
    expect(mockCreateTrack).not.toHaveBeenCalled();
  });

  // AC#11: Track-to-artist lateral navigation
  it('shows tappable artist link on track detail', () => {
    setDetailHandoff(_result({ subtitle: 'M83' }));
    const { getByTestId } = renderDetail();
    expect(getByTestId('detail-artist-link')).toBeTruthy();
  });

  it('searches for artist when artist link is tapped', async () => {
    mockSearchDiscovery.mockResolvedValueOnce({ results: [] });
    setDetailHandoff(_result({ subtitle: 'M83' }));
    const { getByTestId } = renderDetail();
    fireEvent.press(getByTestId('detail-artist-link'));
    await waitFor(() =>
      expect(mockSearchDiscovery).toHaveBeenCalledWith({ q: 'M83', kinds: ['artist'], limit: 1, saveHistory: false }),
    );
  });

  // AC#12: Track-to-album lateral navigation
  it('shows tappable album row when extras has album', () => {
    setDetailHandoff(_result({ extras: { album: 'Hurry Up, We\'re Dreaming' } }));
    const { getByTestId } = renderDetail();
    expect(getByTestId('detail-info-album')).toBeTruthy();
  });

  it('searches for album when album row is tapped', async () => {
    mockSearchDiscovery.mockResolvedValueOnce({ results: [] });
    setDetailHandoff(_result({ subtitle: 'M83', extras: { album: 'Hurry Up' } }));
    const { getByTestId } = renderDetail();
    fireEvent.press(getByTestId('detail-info-album'));
    await waitFor(() =>
      expect(mockSearchDiscovery).toHaveBeenCalledWith({
        q: 'Hurry Up M83',
        kinds: ['album'],
        limit: 1,
        saveHistory: false,
      }),
    );
  });

  // AC#13: Album-to-artist lateral navigation
  it('shows tappable artist link on album detail', () => {
    setDetailHandoff(_result({ kind: 'album', subtitle: 'The Weeknd' }));
    const { getByTestId } = renderDetail();
    expect(getByTestId('detail-artist-link')).toBeTruthy();
  });

  it('does not show artist link on artist detail (no lateral nav to self)', () => {
    setDetailHandoff(_result({ kind: 'artist', subtitle: null }));
    const { queryByTestId } = renderDetail();
    expect(queryByTestId('detail-artist-link')).toBeNull();
  });
});
