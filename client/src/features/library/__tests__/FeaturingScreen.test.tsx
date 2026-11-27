import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ComponentProps, ReactNode } from 'react';

import type { Navigator } from '@shared/navigation';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { AcquisitionPortProvider } from '@shared/acquisition';
import { asTrackId } from '@shared/catalog';
import { ApiError, NetworkError } from '@shared/failure';
import { warn } from '@shared/log';
import { recordLogs, type RecordedLogs } from '@testing/fakes/log/inMemoryLogger';

import { LibraryPortProvider, FeaturingScreen } from '@features/library';
import { createMemoryLibraryPort } from '@testing/fakes/library/inMemoryLibraryPort';
import {
  recordingDialogs,
  type RecordingDialogs,
} from '@features/library/__tests__/recordingDialogs';
import type { LibraryPort } from '@features/library';
import { readDetailHandoff } from '@shared/navigation';
import { trackResponseFactory } from '@testing/factories';
import { signInTestUser } from '@testing/signInTestUser';
import { PlaybackContext } from '@shared/playback';
import { idlePlayback } from '@testing/queryHarness';
import { apiClientAcquisitionPort } from '@testing/apiClientPorts';

const { __http } = require('@testing/doubles/fetch.js');

const EVENTS = 'POST /v1/discovery/events';

const mockSearchDiscovery = jest.fn();
const mockListTracksFeaturing = jest.fn();

function portRecordingInto(dialogs: RecordingDialogs): LibraryPort {
  return {
    ...createMemoryLibraryPort({ warn, showFailure: dialogs.showFailure }),
    searchDiscovery: (params) => mockSearchDiscovery(params),
    listTracksFeaturing: (featured) => mockListTracksFeaturing(featured),
  };
}

type FeaturingProps = ComponentProps<typeof FeaturingScreen>;

function featuringProps(overrides: Partial<FeaturingProps> = {}): FeaturingProps {
  return {
    params: { name: 'Guest Star' },
    segments: ['(tabs)', 'library', 'featuring'],
    navigator: recordingNavigator(),
    ...overrides,
  };
}

type RecordingNavigator = Navigator & { push: jest.Mock };

function recordingNavigator(): RecordingNavigator {
  return { push: jest.fn(), replace: jest.fn(), back: jest.fn(), canGoBack: () => false };
}

const SECRET = 'guest star unreleased bootleg';
const SERVICE_UNAVAILABLE = 503;

let client: QueryClient;
let logs: RecordedLogs;
let dialogs: RecordingDialogs;
let port: LibraryPort;

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={client}>
      <PlaybackContext.Provider value={idlePlayback}>
        <AcquisitionPortProvider port={apiClientAcquisitionPort}>
          <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
        </AcquisitionPortProvider>
      </PlaybackContext.Provider>
    </QueryClientProvider>
  );
}

beforeEach(async () => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  logs = recordLogs();
  dialogs = recordingDialogs();
  port = portRecordingInto(dialogs);
  await signInTestUser();
  __http.reply(EVENTS, { status: 204 });
  mockSearchDiscovery.mockReset();
  mockListTracksFeaturing.mockReset();
  mockListTracksFeaturing.mockResolvedValue({ items: [] });
});

afterEach(() => {
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
    mockSearchDiscovery.mockRejectedValue(
      new ApiError(SERVICE_UNAVAILABLE, SECRET, 'discovery.all_providers_failed', 'corr-7'),
    );
    render(<FeaturingScreen {...featuringProps()} />, { wrapper });

    await tapExplore();

    await waitFor(() =>
      expect(logs.warnings).toContainEqual([
        '[library] featuring explore search failed',
        {
          status: 503,
          code: 'discovery.all_providers_failed',
          failure: 'server',
          corr_id: 'corr-7',
        },
      ]),
    );
  });

  it('tells the user the search failed instead of leaving the tap silent', async () => {
    mockSearchDiscovery.mockRejectedValue(new NetworkError('transport', `search ${SECRET} failed`));
    render(<FeaturingScreen {...featuringProps()} />, { wrapper });

    await tapExplore();

    await waitFor(() =>
      expect(dialogs.shown()).toEqual([
        ['Search failed', 'Could not search for Guest Star. Please try again.'],
      ]),
    );
    expect(logs.text()).not.toContain(SECRET);
  });

  it('asks the user to sign in again when the search is refused for auth', async () => {
    mockSearchDiscovery.mockRejectedValue(new ApiError(403, 'forbidden'));
    render(<FeaturingScreen {...featuringProps()} />, { wrapper });

    await tapExplore();

    await waitFor(() =>
      expect(dialogs.shown()).toEqual([
        ['Search failed', 'Could not search for Guest Star. Sign in again, then retry.'],
      ]),
    );
  });

  it('offers the search again once the failed one has settled', async () => {
    mockSearchDiscovery.mockRejectedValue(new NetworkError('transport', 'offline'));
    render(<FeaturingScreen {...featuringProps()} />, { wrapper });

    await tapExplore();

    await waitFor(() => expect(screen.getByText('Search for Guest Star')).toBeTruthy());
  });

  it('logs nothing and alerts nothing when the search answers', async () => {
    mockSearchDiscovery.mockResolvedValue({ results: [] });
    render(<FeaturingScreen {...featuringProps()} />, { wrapper });

    await tapExplore();

    await waitFor(() =>
      expect(mockSearchDiscovery).toHaveBeenCalledWith({
        q: 'Guest Star',
        kinds: ['artist', 'track'],
        limit: 1,
        saveHistory: false,
      }),
    );
    expect(logs.warnings).toEqual([]);
    expect(dialogs.alerts).toEqual([]);
  });
});

describe('a failed featuring load', () => {
  it('logs the query key, the status and the failure class behind the generic copy', async () => {
    mockListTracksFeaturing.mockRejectedValue(new ApiError(500, SECRET, undefined, 'corr-9'));
    render(<FeaturingScreen {...featuringProps()} />, { wrapper });

    await waitFor(() => expect(screen.getByText("Couldn't load tracks.")).toBeTruthy());

    expect(logs.warnings).toContainEqual([
      '[library] featuring query failed',
      { key: 'name', status: 500, failure: 'server', corr_id: 'corr-9' },
    ]);
    expect(logs.text()).not.toContain(SECRET);
  });

  it('classifies an unreachable API as a network failure', async () => {
    mockListTracksFeaturing.mockRejectedValue(new NetworkError('transport', 'API unreachable'));
    render(<FeaturingScreen {...featuringProps()} />, { wrapper });

    await waitFor(() =>
      expect(logs.warnings).toContainEqual([
        '[library] featuring query failed',
        { key: 'name', failure: 'network' },
      ]),
    );
  });

  it('logs nothing while the load keeps answering', async () => {
    render(<FeaturingScreen {...featuringProps()} />, { wrapper });

    await waitFor(() =>
      expect(mockListTracksFeaturing).toHaveBeenCalledWith(
        expect.objectContaining({ name: 'Guest Star' }),
      ),
    );
    expect(logs.warnings).toEqual([]);
  });
});

type PushedHref = { pathname: string; params: { handoff: string } };

describe('the detail route under the library tab', () => {
  const pendingTrack = trackResponseFactory.build({
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
  });

  let navigator: ReturnType<typeof recordingNavigator>;
  let push: jest.Mock;

  beforeEach(() => {
    navigator = recordingNavigator();
    push = navigator.push;
  });

  it('opens a track at the library detail path, not the discover one', async () => {
    mockListTracksFeaturing.mockResolvedValue({ items: [pendingTrack] });
    render(<FeaturingScreen {...featuringProps({ navigator })} />, { wrapper });

    const row = await screen.findByTestId(`library-row-${pendingTrack.id}`);
    fireEvent.press(row);

    expect(push).toHaveBeenCalledTimes(1);
    const href = push.mock.calls[0][0] as PushedHref;
    expect(href.pathname).toBe('/library/detail');
    expect(readDetailHandoff(href.params.handoff)?.result).toMatchObject({ title: 'Aerodynamic' });
  });

  it('sends the explore search to the library detail path, not the discover one', async () => {
    mockSearchDiscovery.mockResolvedValue({ results: [{ kind: 'artist', title: 'Guest Star' }] });
    render(<FeaturingScreen {...featuringProps({ navigator })} />, { wrapper });

    await tapExplore();

    await waitFor(() => expect(push).toHaveBeenCalledTimes(1));
    const href = push.mock.calls[0][0] as PushedHref;
    expect(href.pathname).toBe('/library/detail');
    expect(readDetailHandoff(href.params.handoff)?.result).toMatchObject({
      kind: 'artist',
      title: 'Guest Star',
    });
  });
});

describe('the featuring screen opened from the discover tab', () => {
  let navigator: ReturnType<typeof recordingNavigator>;
  let push: jest.Mock;

  const oddTrack = trackResponseFactory.build({
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
  });

  const discoverTab = ['(tabs)', 'discover', 'featuring'];

  beforeEach(() => {
    navigator = recordingNavigator();
    push = navigator.push;
  });

  it('opens a tapped track on the discover detail screen', async () => {
    mockListTracksFeaturing.mockResolvedValue({ items: [oddTrack] });
    render(<FeaturingScreen {...featuringProps({ navigator, segments: discoverTab })} />, {
      wrapper,
    });

    fireEvent.press(await screen.findByTestId(`library-row-${oddTrack.id}`));

    expect(push).toHaveBeenCalledTimes(1);
    expect(push.mock.calls[0][0]).toMatchObject({ pathname: '/discover/detail' });
  });

  it('hands the detail screen a track whose title has slashes, query and hash characters intact', async () => {
    mockListTracksFeaturing.mockResolvedValue({ items: [oddTrack] });
    render(<FeaturingScreen {...featuringProps({ navigator, segments: discoverTab })} />, {
      wrapper,
    });

    fireEvent.press(await screen.findByTestId(`library-row-${oddTrack.id}`));

    const href = push.mock.calls[0][0] as { params: { handoff: string } };
    expect(readDetailHandoff(href.params.handoff)?.result).toMatchObject({
      title: 'Around the World / Harder, Better? #1 & "more"',
    });
  });

  it('sends the explore search result to the discover detail screen', async () => {
    mockSearchDiscovery.mockResolvedValue({ results: [{ kind: 'artist', title: 'Guest Star' }] });
    render(<FeaturingScreen {...featuringProps({ navigator, segments: discoverTab })} />, {
      wrapper,
    });

    await tapExplore();

    await waitFor(() => expect(push).toHaveBeenCalledTimes(1));
    expect(push.mock.calls[0][0]).toMatchObject({ pathname: '/discover/detail' });
  });

  it('searches for an artist name with special characters and opens it on the discover detail screen', async () => {
    const name = 'AC/DC & Friends? #1';
    mockSearchDiscovery.mockResolvedValue({ results: [{ kind: 'artist', title: name }] });
    render(
      <FeaturingScreen
        {...featuringProps({ navigator, segments: discoverTab, params: { name } })}
      />,
      { wrapper },
    );

    await tapExplore();

    await waitFor(() => expect(push).toHaveBeenCalledTimes(1));
    const href = push.mock.calls[0][0] as PushedHref;
    expect(href.pathname).toBe('/discover/detail');
    expect(readDetailHandoff(href.params.handoff)?.result).toMatchObject({
      kind: 'artist',
      title: 'AC/DC & Friends? #1',
    });
  });
});

describe('featuring route params', () => {
  it('requests one value per param when the route repeats name and mbid', async () => {
    render(
      <FeaturingScreen {...featuringProps({ params: { name: ['a', 'b'], mbid: ['x', 'y'] } })} />,
      {
        wrapper,
      },
    );

    await waitFor(() =>
      expect(mockListTracksFeaturing).toHaveBeenCalledWith({
        name: 'a',
        mbid: 'x',
        deezer_id: null,
      }),
    );
  });

  it('shows an incomplete-link state and no empty-name search when no param is usable', async () => {
    render(<FeaturingScreen {...featuringProps({ params: {} })} />, { wrapper });

    expect(await screen.findByText(/link is incomplete/)).toBeTruthy();
    expect(screen.queryByTestId('featuring-explore')).toBeNull();
    expect(screen.queryByText(/featuring\s+yet/)).toBeNull();
    expect(mockListTracksFeaturing).not.toHaveBeenCalled();
  });

  it('tells the user to sign in again when the load is refused with 401', async () => {
    mockListTracksFeaturing.mockRejectedValue(new ApiError(401, 'unauthorized'));
    render(<FeaturingScreen {...featuringProps()} />, { wrapper });

    expect(
      await screen.findByText("Couldn't load tracks. Sign in again, then retry."),
    ).toBeTruthy();
  });

  it('keeps the plain retry copy when the load fails on the network', async () => {
    mockListTracksFeaturing.mockRejectedValue(new NetworkError('transport', 'offline'));
    render(<FeaturingScreen {...featuringProps()} />, { wrapper });

    expect(await screen.findByText("Couldn't load tracks.")).toBeTruthy();
    expect(screen.getByText('Retry')).toBeTruthy();
  });
});
