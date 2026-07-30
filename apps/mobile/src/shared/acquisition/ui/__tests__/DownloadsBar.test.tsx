import { Animated } from 'react-native';

import { render, screen, fireEvent } from '@testing-library/react-native';

import { DownloadsBar, deriveBarDisplay } from '../DownloadsBar';
import type { DownloadEntry } from '@shared/acquisition/downloadStore';
import { ACQUISITION_PHASES } from '@shared/acquisition/stagePhase';
import * as announceModule from '@shared/ui/announce';

function entry(overrides: Partial<DownloadEntry>): DownloadEntry {
  return {
    trackId: 't1',
    phase: 'downloading',
    title: 'Track title',
    artist: 'Some artist',
    artworkUrl: null,
    ...overrides,
  };
}

describe('deriveBarDisplay', () => {
  it('names the single track when exactly one item is downloading', () => {
    const items = [entry({ trackId: 'a', phase: 'downloading', title: 'Midnight City' })];

    const result = deriveBarDisplay(items);

    expect(result.phase).toBe('downloading');
    expect(result.count).toBe(1);
    expect(result.heading).toBe('Downloading "Midnight City"');
    expect(result.activeIndex).toBe(ACQUISITION_PHASES.indexOf('downloading'));
  });

  it('falls back to a generic label when the sole item has no title yet', () => {
    const items = [entry({ trackId: 'a', phase: 'finding', title: null })];

    const result = deriveBarDisplay(items);

    expect(result.heading).toBe('Downloading "track"');
  });

  it('pluralizes the heading and counts only active items when several are downloading', () => {
    const items = [
      entry({ trackId: 'a', phase: 'downloading' }),
      entry({ trackId: 'b', phase: 'finishing' }),
      entry({ trackId: 'c', phase: 'done' }),
    ];

    const result = deriveBarDisplay(items);

    expect(result.count).toBe(2);
    expect(result.heading).toBe('Downloading 2 tracks');
  });

  it('reports Done with activeIndex past the last phase when every item is done', () => {
    const items = [
      entry({ trackId: 'a', phase: 'done' }),
      entry({ trackId: 'b', phase: 'done' }),
    ];

    const result = deriveBarDisplay(items);

    expect(result.phase).toBe('done');
    expect(result.count).toBe(2);
    expect(result.heading).toBe('Done');
    expect(result.activeIndex).toBe(ACQUISITION_PHASES.length);
  });

  it('counts only the still-active items in a mixed batch of done and active entries', () => {
    const items = [
      entry({ trackId: 'a', phase: 'done' }),
      entry({ trackId: 'b', phase: 'downloading' }),
      entry({ trackId: 'c', phase: 'finding' }),
    ];

    const result = deriveBarDisplay(items);

    expect(result.count).toBe(2);
    expect(result.phase).toBe('finding');
    expect(result.heading).toBe('Downloading 2 tracks');
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
    const items = [entry({ trackId: 'a', phase: 'downloading', title: 'Nightcall' })];

    render(<DownloadsBar items={items} onPress={jest.fn()} />);

    expect(screen.getByText('Downloading "Nightcall"')).toBeTruthy();
    expect(screen.getByText('Downloading…')).toBeTruthy();
  });

  it('exposes an accessible button whose label reflects the current heading and phase', () => {
    const items = [entry({ trackId: 'a', phase: 'finishing', title: 'Instant Crush' })];

    render(<DownloadsBar items={items} onPress={jest.fn()} />);

    expect(
      screen.getByRole('button', {
        name: 'Downloading "Instant Crush". Finishing up…. Tap to expand.',
      }),
    ).toBeTruthy();
  });

  it('calls onPress when the bar is pressed', () => {
    const onPress = jest.fn();
    const items = [entry({ trackId: 'a', phase: 'downloading' })];
    render(<DownloadsBar items={items} onPress={onPress} />);

    fireEvent.press(screen.getByRole('button'));

    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('announces an empty string, not a phantom heading, when the item list goes back to empty', () => {
    const announceSpy = jest.spyOn(announceModule, 'announce');
    const items = [entry({ trackId: 'a', phase: 'downloading', title: 'Nightcall' })];
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
      .mockReturnValue({ start: jest.fn(), stop, reset: jest.fn() } as unknown as ReturnType<
        typeof Animated.loop
      >);

    const items = [entry({ trackId: 'a', phase: 'downloading' })];
    const { unmount } = render(<DownloadsBar items={items} onPress={jest.fn()} />);
    expect(stop).not.toHaveBeenCalled();

    unmount();

    expect(stop).toHaveBeenCalledTimes(1);
    loopSpy.mockRestore();
  });
});
