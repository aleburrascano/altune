import { Animated } from 'react-native';

import { act, fireEvent, render, renderHook, screen } from '@testing-library/react-native';

import { DownloadsBar, deriveBarDisplay } from '../DownloadsBar';
import {
  FAILED_HOLD_MS,
  FINISHING_DWELL_MS,
  useActiveDownloadItems,
  useDownloadStore,
  type DownloadEntry,
} from '@shared/acquisition/downloadStore';
import { ACQUISITION_PHASES } from '@shared/acquisition/stagePhase';
import * as announceModule from '@shared/ui/announce';
import { asTrackId } from '@shared/api-client/ids';

function entry(overrides: Partial<DownloadEntry>): DownloadEntry {
  return {
    trackId: asTrackId('t1'),
    phase: 'downloading',
    title: 'Track title',
    artist: 'Some artist',
    artworkUrl: null,
    ...overrides,
  };
}

describe('deriveBarDisplay', () => {
  it('names the single track when exactly one item is downloading', () => {
    const items = [
      entry({ trackId: asTrackId('a'), phase: 'downloading', title: 'Midnight City' }),
    ];

    const result = deriveBarDisplay(items);

    expect(result.phase).toBe('downloading');
    expect(result.count).toBe(1);
    expect(result.heading).toBe('Downloading "Midnight City"');
    expect(result.activeIndex).toBe(ACQUISITION_PHASES.indexOf('downloading'));
  });

  it('falls back to a generic label when the sole item has no title yet', () => {
    const items = [entry({ trackId: asTrackId('a'), phase: 'finding', title: null })];

    const result = deriveBarDisplay(items);

    expect(result.heading).toBe('Downloading "track"');
  });

  it('pluralizes the heading and counts only active items when several are downloading', () => {
    const items = [
      entry({ trackId: asTrackId('a'), phase: 'downloading' }),
      entry({ trackId: asTrackId('b'), phase: 'finishing' }),
      entry({ trackId: asTrackId('c'), phase: 'done' }),
    ];

    const result = deriveBarDisplay(items);

    expect(result.count).toBe(2);
    expect(result.heading).toBe('Downloading 2 tracks');
  });

  it('reports Done with activeIndex past the last phase when every item is done', () => {
    const items = [
      entry({ trackId: asTrackId('a'), phase: 'done' }),
      entry({ trackId: asTrackId('b'), phase: 'done' }),
    ];

    const result = deriveBarDisplay(items);

    expect(result.phase).toBe('done');
    expect(result.count).toBe(2);
    expect(result.heading).toBe('Done');
    expect(result.activeIndex).toBe(ACQUISITION_PHASES.length);
  });

  it('counts only the still-active items in a mixed batch of done and active entries', () => {
    const items = [
      entry({ trackId: asTrackId('a'), phase: 'done' }),
      entry({ trackId: asTrackId('b'), phase: 'downloading' }),
      entry({ trackId: asTrackId('c'), phase: 'finding' }),
    ];

    const result = deriveBarDisplay(items);

    expect(result.count).toBe(2);
    expect(result.phase).toBe('finding');
    expect(result.heading).toBe('Downloading 2 tracks');
  });

  it('reports the failed count instead of plain Done when a settled batch had a failure', () => {
    const items = [
      entry({ trackId: asTrackId('a'), phase: 'done' }),
      entry({ trackId: asTrackId('b'), phase: 'failed' }),
      entry({ trackId: asTrackId('c'), phase: 'done' }),
    ];

    const result = deriveBarDisplay(items);

    expect(result.phase).toBe('failed');
    expect(result.heading).toBe('Done, 1 failed');
    expect(result.activeIndex).toBe(ACQUISITION_PHASES.length);
  });

  it('keeps failed items out of the in-flight count and phase, but names them in the heading', () => {
    const items = [
      entry({ trackId: asTrackId('a'), phase: 'failed' }),
      entry({ trackId: asTrackId('b'), phase: 'downloading' }),
      entry({ trackId: asTrackId('c'), phase: 'finishing' }),
    ];

    const result = deriveBarDisplay(items);

    expect(result.phase).toBe('downloading');
    expect(result.count).toBe(2);
    expect(result.heading).toBe('Downloading 2 tracks, 1 failed');
  });

  it('reports only the failure when every item in the batch failed', () => {
    const items = [
      entry({ trackId: asTrackId('a'), phase: 'failed' }),
      entry({ trackId: asTrackId('b'), phase: 'failed' }),
    ];

    const result = deriveBarDisplay(items);

    expect(result.phase).toBe('failed');
    expect(result.count).toBe(2);
    expect(result.heading).toBe('2 failed');
  });

  it('reports zero count and the plural heading form for an empty item list', () => {
    const result = deriveBarDisplay([]);

    expect(result.phase).toBe('finding');
    expect(result.count).toBe(0);
    expect(result.heading).toBe('Downloading 0 tracks');
  });
});

describe('DownloadsBar', () => {
  it('renders null and nothing to the screen when there are no items', () => {
    const { toJSON } = render(<DownloadsBar items={[]} onPress={jest.fn()} />);

    expect(toJSON()).toBeNull();
  });

  it('renders the derived heading and phase label for the current items', () => {
    const items = [entry({ trackId: asTrackId('a'), phase: 'downloading', title: 'Nightcall' })];

    render(<DownloadsBar items={items} onPress={jest.fn()} />);

    expect(screen.getByText('Downloading "Nightcall"')).toBeTruthy();
    expect(screen.getByText('Downloading…')).toBeTruthy();
  });

  it('exposes an accessible button whose label reflects the current heading and phase', () => {
    const items = [entry({ trackId: asTrackId('a'), phase: 'finishing', title: 'Instant Crush' })];

    render(<DownloadsBar items={items} onPress={jest.fn()} />);

    expect(
      screen.getByRole('button', {
        name: 'Downloading "Instant Crush". Finishing up…. Tap to expand.',
      }),
    ).toBeTruthy();
  });

  it('calls onPress when the bar is pressed', () => {
    const onPress = jest.fn();
    const items = [entry({ trackId: asTrackId('a'), phase: 'downloading' })];
    render(<DownloadsBar items={items} onPress={onPress} />);

    fireEvent.press(screen.getByRole('button'));

    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('announces an empty string, not a phantom heading, when the item list goes back to empty', () => {
    const announceSpy = jest.spyOn(announceModule, 'announce');
    const items = [entry({ trackId: asTrackId('a'), phase: 'downloading', title: 'Nightcall' })];
    const { rerender } = render(<DownloadsBar items={items} onPress={jest.fn()} />);

    announceSpy.mockClear();
    rerender(<DownloadsBar items={[]} onPress={jest.fn()} />);

    expect(announceSpy).toHaveBeenCalledWith('');
    announceSpy.mockRestore();
  });

  it('stops the pulse animation loop on unmount instead of leaving it running', () => {
    const stop = jest.fn();
    const loopSpy = jest
      .spyOn(Animated, 'loop')
      .mockReturnValue({ start: jest.fn(), stop, reset: jest.fn() });

    const items = [entry({ trackId: asTrackId('a'), phase: 'downloading' })];
    const { unmount } = render(<DownloadsBar items={items} onPress={jest.fn()} />);
    expect(stop).not.toHaveBeenCalled();

    unmount();

    expect(stop).toHaveBeenCalledTimes(1);
    loopSpy.mockRestore();
  });
});

describe('DownloadsBar over the live download store', () => {
  beforeEach(() => {
    jest.useFakeTimers();
    useDownloadStore.getState().reset();
  });

  afterEach(() => {
    useDownloadStore.getState().reset();
    jest.useRealTimers();
  });

  it('lands on a failure count, not plain Done, when 1 of 5 imported tracks fails early', () => {
    const ids = ['t1', 't2', 't3', 't4', 't5'].map(asTrackId);
    const { result } = renderHook(() => useActiveDownloadItems());
    act(() => {
      ids.forEach((id) => useDownloadStore.getState().start(id, { title: id }));
      // One track fails server-side right away...
      useDownloadStore.getState().fail(ids[2]!);
    });
    // ...while the other four take longer than the failed hold window to finish.
    act(() => {
      jest.advanceTimersByTime(FAILED_HOLD_MS * 2);
    });
    act(() => {
      ids.filter((_, i) => i !== 2).forEach((id) => useDownloadStore.getState().complete(id));
      jest.advanceTimersByTime(FINISHING_DWELL_MS);
    });

    render(<DownloadsBar items={result.current} onPress={jest.fn()} />);

    expect(screen.getByText('Done, 1 failed')).toBeTruthy();
    expect(screen.queryByText('Done')).toBeNull();
  });
});
