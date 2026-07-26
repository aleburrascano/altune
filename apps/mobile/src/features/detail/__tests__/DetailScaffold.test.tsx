import { render } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

import { clearDetailHandoff, setDetailHandoff } from '@shared/lib/detail-handoff';
import type { DiscoveryResult } from '../../../shared/api-client/discovery';

jest.mock('expo-image', () => ({ Image: () => null }));
jest.mock('expo-linear-gradient', () => ({ LinearGradient: () => null }));

jest.mock('expo-router', () => ({
  useRouter: () => ({
    back: jest.fn(),
    push: jest.fn(),
    replace: jest.fn(),
    canGoBack: () => true,
  }),
  useSegments: () => ['(tabs)', 'discover', 'detail'],
  Redirect: () => null,
}));

jest.mock('../../../shared/api-client/tracks', () => ({
  createTrack: jest.fn(),
  getTracks: jest.fn(),
}));
jest.mock('../../../shared/api-client/discovery', () => ({ searchDiscovery: jest.fn() }));

const emptyPayload = { items: [], provider: 'deezer', status: 'ok', latency_ms: 1 };
jest.mock('../../../shared/api-client/enrichment', () => ({
  getAlbumTracks: () => Promise.resolve(emptyPayload),
  getArtistTopTracks: () => Promise.resolve(emptyPayload),
  getArtistAlbums: () => Promise.resolve(emptyPayload),
  getRelatedTracks: () => Promise.resolve(emptyPayload),
  getEnrichment: () =>
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
}));

function _result(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: 'Get Lucky',
    subtitle: 'Daft Punk',
    image_url: 'https://img.example/ram.jpg',
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
    createElement(
      QueryClientProvider,
      { client: qc },
      createElement(PlaybackProvider, null, children),
    );
  return render(createElement(DetailScreen), { wrapper });
}

afterEach(() => {
  clearDetailHandoff();
  jest.clearAllMocks();
});

describe('DetailScaffold shape', () => {
  it.each([
    ['track' as const, null],
    ['album' as const, 'Daft Punk'],
    ['artist' as const, null],
  ])('renders the banner and app bar for a %s', (kind, subtitle) => {
    setDetailHandoff(_result({ kind, ...(subtitle !== null ? { subtitle } : {}) }));
    const { getByTestId } = renderDetail();
    expect(getByTestId('detail-header')).toBeTruthy();
    expect(getByTestId('detail-back')).toBeTruthy();
    expect(getByTestId('detail-banner-title')).toBeTruthy();
  });

  it('omits a fact cell whose value is absent', () => {
    setDetailHandoff(_result({ extras: { duration_seconds: 369 } }));
    const { getByTestId } = renderDetail();
    const facts = getByTestId('detail-track-facts');
    expect(facts).toHaveTextContent(/LENGTH/);
    expect(facts).toHaveTextContent(/6:09/);
    expect(facts).not.toHaveTextContent(/RELEASED/);
  });

  it('renders no fact row at all when every value is absent', () => {
    setDetailHandoff(_result({ extras: {} }));
    const { queryByTestId } = renderDetail();
    expect(queryByTestId('detail-track-facts')).toBeNull();
  });

  it('reports a preview source in the fact row instead of a warning banner', () => {
    setDetailHandoff(
      _result({ extras: { preview_url: 'https://cdn.example/p.mp3', duration_seconds: 369 } }),
    );
    const { getByTestId } = renderDetail();
    const facts = getByTestId('detail-track-facts');
    expect(facts).toHaveTextContent(/SOURCE/);
    expect(facts).toHaveTextContent(/Preview/);
    expect(getByTestId('detail-preview')).toBeTruthy();
  });

  it('reports a library source when the owned file is playable', () => {
    setDetailHandoff(
      _result({
        extras: { track_id: 't-1', acquisition_status: 'ready', duration_seconds: 369 },
      }),
    );
    const { getByTestId } = renderDetail();
    expect(getByTestId('detail-track-facts')).toHaveTextContent(/Library/);
    expect(getByTestId('detail-play')).toBeTruthy();
  });

  it('offers Wrong album? in the overflow menu, not the body', () => {
    setDetailHandoff(_result({ extras: { album: 'Random Access Memories' } }));
    const { getByTestId, queryByText } = renderDetail();
    expect(getByTestId('detail-menu')).toBeTruthy();
    expect(queryByText('Wrong album?')).toBeNull();
  });

  it('hides the overflow menu when the track has no album to report', () => {
    setDetailHandoff(_result({ extras: {} }));
    const { queryByTestId } = renderDetail();
    expect(queryByTestId('detail-menu')).toBeNull();
  });

  it('names the album name once, as a Details row', () => {
    setDetailHandoff(_result({ extras: { album: 'Random Access Memories' } }));
    const { getAllByText } = renderDetail();
    expect(getAllByText('Random Access Memories')).toHaveLength(1);
  });
});
