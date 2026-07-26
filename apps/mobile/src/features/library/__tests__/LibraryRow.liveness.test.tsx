import { act, render } from '@testing-library/react-native';

import { LibraryRow } from '../ui/LibraryRow';
import {
  completeDownload,
  progressDownload,
  startDownload,
  useDownloadStore,
} from '@shared/acquisition/downloadStore';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import type { TrackResponse } from '../../../shared/api-client/types';

jest.mock('expo-image', () => ({ Image: () => null }));

function _track(): TrackResponse {
  return {
    id: 't1',
    title: 'Midnight City',
    artist: 'M83',
    album: null,
    duration_seconds: null,
    added_at: '2026-05-01T12:00:00Z',
    acquisition_status: 'pending',
    artwork_url: null,
    failure_reason: null,
    failure_message: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
  };
}

const noop = (): void => {};

function setPinned(trackId: string, status: 'queued' | 'downloading' | 'ready'): void {
  usePinnedStore.setState((s) => ({
    entries: { ...s.entries, [trackId]: { trackId, status } },
  }));
}

beforeEach(() => {
  useDownloadStore.getState().reset();
  usePinnedStore.setState({ entries: {} });
});

describe('LibraryRow liveness', () => {
  it('follows the download phase without a new render input', () => {
    const { getByTestId, queryByTestId } = render(
      <LibraryRow track={_track()} onPress={noop} onMore={noop} />,
    );

    act(() => {
      startDownload('t1');
    });
    expect(getByTestId('library-row-pending-t1')).toHaveTextContent(/Finding source/);

    act(() => {
      progressDownload('t1', 'downloading');
    });
    expect(getByTestId('library-row-pending-t1')).toHaveTextContent(/Downloading/);

    act(() => {
      completeDownload('t1');
    });
    expect(queryByTestId('library-row-pending-t1')).not.toHaveTextContent(/Downloading/);
  });

  it('follows the offline pin status without a new render input', () => {
    const { getByTestId } = render(<LibraryRow track={_track()} onPress={noop} onMore={noop} />);
    const label = (): string => getByTestId('library-row-t1').props.accessibilityLabel;

    expect(label()).not.toMatch(/download/);

    act(() => {
      setPinned('t1', 'downloading');
    });
    expect(label()).toMatch(/, downloading/);

    act(() => {
      setPinned('t1', 'ready');
    });
    expect(label()).toMatch(/, downloaded/);
    expect(label()).not.toMatch(/, downloading/);
  });

  it('does not react to another track changing', () => {
    const { getByTestId } = render(<LibraryRow track={_track()} onPress={noop} onMore={noop} />);

    act(() => {
      startDownload('other-track');
      setPinned('other-track', 'ready');
    });

    expect(getByTestId('library-row-t1').props.accessibilityLabel).not.toMatch(/download/);
    expect(getByTestId('library-row-pending-t1')).toHaveTextContent(/Pending/);
  });
});
