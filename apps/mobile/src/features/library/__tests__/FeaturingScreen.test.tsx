import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { Alert } from 'react-native';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';
import { ApiError, NetworkError } from '@shared/errors';

import { FeaturingScreen } from '../ui/FeaturingScreen';
import * as detailHandoff from '@shared/lib/detail-handoff';

const mockSearchDiscovery = jest.fn();
const mockListTracksFeaturing = jest.fn();

jest.mock('@shared/api-client/discovery', () => ({
  searchDiscovery: (params: unknown) => mockSearchDiscovery(params),
}));
jest.mock('@shared/api-client/tracks', () => ({
  listTracksFeaturing: (fa: unknown) => mockListTracksFeaturing(fa),
  deleteTrack: jest.fn(),
  retryAcquisition: jest.fn(),
  reacquireTrack: jest.fn(),
}));

jest.mock('expo-router', () => ({
  useLocalSearchParams: () => ({ name: 'Guest Star' }),
  useRouter: () => ({ push: jest.fn(), replace: jest.fn(), back: jest.fn() }),
  useSegments: () => ['(tabs)', 'library', 'featuring'],
}));

jest.mock('@shared/playback/usePlayback', () => ({
  usePlayback: () => ({ status: 'idle', source: null }),
}));
jest.mock('@shared/playback/useQueuePlayback', () => ({
  useQueuePlayback: () => ({ playFromList: jest.fn() }),
}));

const SECRET = 'guest star unreleased bootleg';

let client: QueryClient;
let warnSpy: jest.SpyInstance;
let alertSpy: jest.SpyInstance;

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function loggedText(): string {
  return JSON.stringify(warnSpy.mock.calls, (_key, value: unknown) =>
    value instanceof Error ? `${value.name}: ${value.message} ${value.stack ?? ''}` : value,
  );
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
  mockSearchDiscovery.mockReset();
  mockListTracksFeaturing.mockReset();
  mockListTracksFeaturing.mockResolvedValue({ items: [] });
});

afterEach(() => {
  warnSpy.mockRestore();
  alertSpy.mockRestore();
  client.clear();
});

async function tapExplore(): Promise<void> {
  const button = await screen.findByTestId('featuring-explore');
  await act(async () => {
    fireEvent.press(button);
  });
}

describe('a failed "Explore artist" search', () => {
  it('logs the artist, the status and the failure class', async () => {
    mockSearchDiscovery.mockRejectedValue(new ApiError(503, SECRET, 'discovery_down', 'corr-7'));
    render(<FeaturingScreen />, { wrapper });

    await tapExplore();

    await waitFor(() =>
      expect(warnSpy).toHaveBeenCalledWith('[library] featuring explore search failed', {
        status: 503,
        code: 'discovery_down',
        failure: 'server',
        correlationId: 'corr-7',
      }),
    );
  });

  it('tells the user the search failed instead of leaving the tap silent', async () => {
    mockSearchDiscovery.mockRejectedValue(new NetworkError('transport', `search ${SECRET} failed`));
    render(<FeaturingScreen />, { wrapper });

    await tapExplore();

    await waitFor(() =>
      expect(alertSpy).toHaveBeenCalledWith(
        'Search failed',
        'Could not search for Guest Star. Please try again.',
      ),
    );
    expect(loggedText()).not.toContain(SECRET);
  });

  it('asks the user to sign in again when the search is refused for auth', async () => {
    mockSearchDiscovery.mockRejectedValue(new ApiError(403, 'forbidden'));
    render(<FeaturingScreen />, { wrapper });

    await tapExplore();

    await waitFor(() =>
      expect(alertSpy).toHaveBeenCalledWith(
        'Search failed',
        'Could not search for Guest Star. Sign in again, then retry.',
      ),
    );
  });

  it('offers the search again once the failed one has settled', async () => {
    mockSearchDiscovery.mockRejectedValue(new NetworkError('transport', 'offline'));
    render(<FeaturingScreen />, { wrapper });

    await tapExplore();

    await waitFor(() => expect(screen.getByText('Search for Guest Star')).toBeTruthy());
  });

  it('logs nothing and alerts nothing when the search answers', async () => {
    mockSearchDiscovery.mockResolvedValue({ results: [] });
    render(<FeaturingScreen />, { wrapper });

    await tapExplore();

    await waitFor(() => expect(mockSearchDiscovery).toHaveBeenCalled());
    expect(warnSpy).not.toHaveBeenCalled();
    expect(alertSpy).not.toHaveBeenCalled();
  });
});

describe('a failed featuring load', () => {
  it('logs the query key, the status and the failure class behind the generic copy', async () => {
    mockListTracksFeaturing.mockRejectedValue(new ApiError(500, SECRET, undefined, 'corr-9'));
    render(<FeaturingScreen />, { wrapper });

    await waitFor(() => expect(screen.getByText("Couldn't load tracks.")).toBeTruthy());

    expect(warnSpy).toHaveBeenCalledWith('[library] featuring query failed', {
      key: 'name',
      status: 500,
      failure: 'server',
      correlationId: 'corr-9',
    });
    expect(loggedText()).not.toContain(SECRET);
  });

  it('classifies an unreachable API as a network failure', async () => {
    mockListTracksFeaturing.mockRejectedValue(new NetworkError('transport', 'API unreachable'));
    render(<FeaturingScreen />, { wrapper });

    await waitFor(() =>
      expect(warnSpy).toHaveBeenCalledWith('[library] featuring query failed', {
        key: 'name',
        failure: 'network',
      }),
    );
  });

  it('logs nothing while the load keeps answering', async () => {
    render(<FeaturingScreen />, { wrapper });

    await waitFor(() => expect(mockListTracksFeaturing).toHaveBeenCalled());
    expect(warnSpy).not.toHaveBeenCalled();
  });
});

describe('the detail route under the library tab', () => {
  const pendingTrack = {
    id: asTrackId('t1'),
    title: 'Aerodynamic',
    artist: 'Daft Punk',
    album: 'Discovery',
    duration_seconds: 212,
    added_at: '2026-01-01T00:00:00Z',
    artwork_url: null,
    year: 2001,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
    acquisition_status: 'pending',
    failure_reason: null,
  };

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('opens a track at the library detail path, not the discover one', async () => {
    const detailHrefSpy = jest.spyOn(detailHandoff, 'detailHref');
    mockListTracksFeaturing.mockResolvedValue({ items: [pendingTrack] });
    render(<FeaturingScreen />, { wrapper });

    const row = await screen.findByTestId(`library-row-${pendingTrack.id}`);
    fireEvent.press(row);

    expect(detailHrefSpy).toHaveBeenCalledWith('/library/detail', expect.anything());
    detailHrefSpy.mockRestore();
  });

  it('sends the explore search to the library detail path, not the discover one', async () => {
    const detailHrefSpy = jest.spyOn(detailHandoff, 'detailHref');
    mockSearchDiscovery.mockResolvedValue({ results: [{ kind: 'artist', title: 'Guest Star' }] });
    render(<FeaturingScreen />, { wrapper });

    await tapExplore();

    await waitFor(() => expect(detailHrefSpy).toHaveBeenCalled());
    expect(detailHrefSpy).toHaveBeenCalledWith('/library/detail', expect.anything());
    detailHrefSpy.mockRestore();
  });
});

describe('the featuring screen opened from the discover tab', () => {
  const router = jest.requireMock('expo-router') as {
    useRouter: () => { push: jest.Mock };
    useSegments: () => string[];
    useLocalSearchParams: () => { name: string };
  };
  const original = { ...router };
  let push: jest.Mock;

  const oddTrack = {
    id: asTrackId('t2'),
    title: 'Around the World / Harder, Better? #1 & "more"',
    artist: 'Daft Punk',
    album: 'Homework',
    duration_seconds: 429,
    added_at: '2026-01-01T00:00:00Z',
    artwork_url: null,
    year: 1997,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
    acquisition_status: 'pending',
    failure_reason: null,
  };

  beforeEach(() => {
    push = jest.fn();
    router.useRouter = () => ({ push });
    router.useSegments = () => ['(tabs)', 'discover', 'featuring'];
  });

  afterEach(() => {
    Object.assign(router, original);
  });

  it('opens a tapped track on the discover detail screen', async () => {
    mockListTracksFeaturing.mockResolvedValue({ items: [oddTrack] });
    render(<FeaturingScreen />, { wrapper });

    fireEvent.press(await screen.findByTestId(`library-row-${oddTrack.id}`));

    expect(push).toHaveBeenCalledTimes(1);
    expect(push.mock.calls[0][0]).toMatchObject({ pathname: '/discover/detail' });
  });

  it('hands the detail screen a track whose title has slashes, query and hash characters intact', async () => {
    mockListTracksFeaturing.mockResolvedValue({ items: [oddTrack] });
    render(<FeaturingScreen />, { wrapper });

    fireEvent.press(await screen.findByTestId(`library-row-${oddTrack.id}`));

    const href = push.mock.calls[0][0] as { params: { handoff: string } };
    expect(detailHandoff.readDetailHandoff(href.params.handoff)?.result).toMatchObject({
      title: 'Around the World / Harder, Better? #1 & "more"',
    });
  });

  it('sends the explore search result to the discover detail screen', async () => {
    mockSearchDiscovery.mockResolvedValue({ results: [{ kind: 'artist', title: 'Guest Star' }] });
    render(<FeaturingScreen />, { wrapper });

    await tapExplore();

    await waitFor(() => expect(push).toHaveBeenCalledTimes(1));
    expect(push.mock.calls[0][0]).toMatchObject({ pathname: '/discover/detail' });
  });

  it('searches for an artist name with special characters and opens it on the discover detail screen', async () => {
    const name = 'AC/DC & Friends? #1';
    router.useLocalSearchParams = () => ({ name });
    mockSearchDiscovery.mockResolvedValue({ results: [{ kind: 'artist', title: name }] });
    render(<FeaturingScreen />, { wrapper });

    await tapExplore();

    await waitFor(() => expect(push).toHaveBeenCalledTimes(1));
    const href = push.mock.calls[0][0] as { pathname: string; params: { handoff: string } };
    expect(href.pathname).toBe('/discover/detail');
    expect(detailHandoff.readDetailHandoff(href.params.handoff)?.result).toMatchObject({
      kind: 'artist',
      title: 'AC/DC & Friends? #1',
    });
  });
});

describe('featuring route params', () => {
  const router = jest.requireMock('expo-router') as {
    useLocalSearchParams: () => Record<string, unknown>;
  };
  const original = router.useLocalSearchParams;

  afterEach(() => {
    router.useLocalSearchParams = original;
  });

  it('requests one value per param when the route repeats name and mbid', async () => {
    router.useLocalSearchParams = () => ({ name: ['a', 'b'], mbid: ['x', 'y'] });
    render(<FeaturingScreen />, { wrapper });

    await waitFor(() => expect(mockListTracksFeaturing).toHaveBeenCalled());
    expect((mockListTracksFeaturing.mock.calls[0] as unknown[])[0]).toEqual({
      name: 'a',
      mbid: 'x',
      deezer_id: null,
    });
  });

  it('shows an incomplete-link state and no empty-name search when no param is usable', async () => {
    router.useLocalSearchParams = () => ({});
    render(<FeaturingScreen />, { wrapper });

    expect(await screen.findByText(/link is incomplete/)).toBeTruthy();
    expect(screen.queryByTestId('featuring-explore')).toBeNull();
    expect(screen.queryByText(/featuring\s+yet/)).toBeNull();
    expect(mockListTracksFeaturing).not.toHaveBeenCalled();
  });

  it('tells the user to sign in again when the load is refused with 401', async () => {
    mockListTracksFeaturing.mockRejectedValue(new ApiError(401, 'unauthorized'));
    render(<FeaturingScreen />, { wrapper });

    expect(
      await screen.findByText("Couldn't load tracks. Sign in again, then retry."),
    ).toBeTruthy();
  });

  it('keeps the plain retry copy when the load fails on the network', async () => {
    mockListTracksFeaturing.mockRejectedValue(new NetworkError('transport', 'offline'));
    render(<FeaturingScreen />, { wrapper });

    expect(await screen.findByText("Couldn't load tracks.")).toBeTruthy();
    expect(screen.getByText('Retry')).toBeTruthy();
  });
});
