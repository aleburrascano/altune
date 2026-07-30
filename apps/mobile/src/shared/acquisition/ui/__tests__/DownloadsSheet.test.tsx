import { act, render, screen, fireEvent } from '@testing-library/react-native';

import { DownloadsSheet } from '../DownloadsSheet';
import { useDownloadStore, type DownloadEntry } from '@shared/acquisition/downloadStore';

function entry(overrides: Partial<DownloadEntry>): DownloadEntry {
  return {
    trackId: 't1',
    phase: 'finding',
    title: 'Track title',
    artist: 'Some artist',
    artworkUrl: null,
    ...overrides,
  };
}

describe('DownloadsSheet', () => {
  beforeEach(() => {
    act(() => {
      useDownloadStore.getState().reset();
    });
  });

  afterEach(() => {
    act(() => {
      useDownloadStore.getState().reset();
    });
  });

  it('shows the number of items in progress', () => {
    const items = [entry({ trackId: 't1' }), entry({ trackId: 't2' })];

    render(<DownloadsSheet visible items={items} onClose={jest.fn()} />);

    expect(screen.getByText('2 in progress')).toBeTruthy();
  });

  it("renders a row's phase from the live store rather than the item prop", () => {
    act(() => {
      useDownloadStore.getState().start('t1', { title: 'Live track' });
      useDownloadStore.getState().progress('t1', 'downloading');
    });
    const items = [entry({ trackId: 't1', phase: 'done', title: 'Live track' })];

    render(<DownloadsSheet visible items={items} onClose={jest.fn()} />);

    expect(screen.getByText('Downloading…')).toBeTruthy();
    expect(screen.queryByText('Done')).toBeNull();
  });

  it('updates the rendered phase when the store changes, without new props', () => {
    act(() => {
      useDownloadStore.getState().start('t1', { title: 'Live track' });
      useDownloadStore.getState().progress('t1', 'downloading');
    });
    const items = [entry({ trackId: 't1', title: 'Live track' })];
    render(<DownloadsSheet visible items={items} onClose={jest.fn()} />);
    expect(screen.getByText('Downloading…')).toBeTruthy();

    act(() => {
      useDownloadStore.getState().progress('t1', 'finishing');
    });

    expect(screen.getByText('Finishing up…')).toBeTruthy();
    expect(screen.queryByText('Downloading…')).toBeNull();
  });

  it('falls back to a working label when the store has no entry for the track', () => {
    const items = [entry({ trackId: 'unknown', phase: 'finding' })];

    render(<DownloadsSheet visible items={items} onClose={jest.fn()} />);

    expect(screen.getByText('Working…')).toBeTruthy();
  });

  it('falls back to generic title and blank artist when the entry has none yet', () => {
    const items = [entry({ trackId: 't1', title: null, artist: null })];

    render(<DownloadsSheet visible items={items} onClose={jest.fn()} />);

    expect(screen.getByText('Track')).toBeTruthy();
  });

  it('exposes an accessible close button that calls onClose when pressed', () => {
    const onClose = jest.fn();
    render(<DownloadsSheet visible items={[]} onClose={onClose} />);

    fireEvent.press(screen.getByRole('button', { name: 'Close downloads' }));

    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
