// #1706: both of this screen's failures were swallowed. The explore search's rejection
// was discarded by a catch-less try/finally — no log, and a tap that read as a dead
// button — and the featuring query answered every failure with the same generic copy and
// no line at all. Redacted like #1703/#1704: the caught error stays out of the log.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { Alert } from 'react-native';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { ApiError, NetworkError } from '@shared/errors';

import { FeaturingScreen } from '../ui/FeaturingScreen';

const mockSearchDiscovery = jest.fn();
const mockListTracksFeaturing = jest.fn();

jest.mock('@shared/api-client/discovery', () => ({
  searchDiscovery: (params: unknown) => mockSearchDiscovery(params),
}));
jest.mock('@shared/api-client/tracks', () => ({
  listTracksFeaturing: () => mockListTracksFeaturing(),
  deleteTrack: jest.fn(),
  retryAcquisition: jest.fn(),
  reacquireTrack: jest.fn(),
}));

jest.mock('expo-router', () => ({
  useLocalSearchParams: () => ({ name: 'Guest Star' }),
  useRouter: () => ({ push: jest.fn(), replace: jest.fn(), back: jest.fn() }),
  useSegments: () => ['(tabs)', 'library', 'featuring'],
}));

// Playback providers are irrelevant to a failed load; stub them so the screen mounts alone.
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

/**
 * What a console would render. `message` and `stack` are non-enumerable, so a plain
 * `JSON.stringify` of a logged Error yields `{}` and would hide the leak this file
 * exists to catch.
 */
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

// The search settles in the same turn as the tap, so the press runs inside `act` to keep
// the button's own "searching" state out of React's unwrapped-update warning.
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
