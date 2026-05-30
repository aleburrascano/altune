/**
 * DetailScreen — header from handoff, empty-handoff redirect, per-kind bodies,
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

jest.mock('expo-image', () => ({ Image: () => null }));

const mockBack = jest.fn();
const mockRedirect = jest.fn((_props: { href: string }) => null);
jest.mock('expo-router', () => ({
  useRouter: () => ({ back: mockBack, push: jest.fn(), replace: jest.fn() }),
  Redirect: (props: { href: string }) => mockRedirect(props),
}));

const mockCreateTrack = jest.fn((_body: unknown) => new Promise<never>(() => {}));
jest.mock('../../../shared/api-client/tracks', () => ({
  createTrack: (body: unknown) => mockCreateTrack(body),
  getTracks: jest.fn(),
}));

import { clearDetailHandoff, setDetailHandoff } from '@shared/lib/detail-handoff';
import type { DiscoveryResult } from '../../../shared/api-client/discovery';

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
  const wrapper = ({ children }: { children: ReactNode }): ReactNode =>
    createElement(QueryClientProvider, { client: qc }, children);
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

  it('renders track info rows for present extras and omits absent keys', () => {
    setDetailHandoff(_result({ extras: { duration_seconds: 244, album: 'After Hours' } }));
    const { getByTestId, queryByTestId } = renderDetail();
    expect(getByTestId('detail-info-duration')).toBeTruthy();
    expect(getByTestId('detail-info-album')).toBeTruthy();
    expect(queryByTestId('detail-info-isrc')).toBeNull();
    expect(queryByTestId('detail-info-popularity')).toBeNull();
  });

  it('shows the tracklist placeholder and no save button for an album', () => {
    setDetailHandoff(_result({ kind: 'album', subtitle: 'The Weeknd', extras: {} }));
    const { getByTestId, queryByTestId } = renderDetail();
    expect(getByTestId('detail-tracklist-placeholder')).toBeTruthy();
    expect(queryByTestId('detail-track-info')).toBeNull();
    expect(queryByTestId('detail-save')).toBeNull();
  });

  it('shows the discography placeholder and no save button for an artist', () => {
    setDetailHandoff(_result({ kind: 'artist', subtitle: null, extras: {} }));
    const { getByTestId, queryByTestId } = renderDetail();
    expect(getByTestId('detail-discography-placeholder')).toBeTruthy();
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
      }),
    );
  });
});
