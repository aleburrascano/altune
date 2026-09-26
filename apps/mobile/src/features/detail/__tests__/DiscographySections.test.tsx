import { fireEvent, render, screen } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { DiscographySections } from '../ui/DiscographySections';

function album(extras: Record<string, unknown>): DiscoveryResult {
  return {
    kind: 'album',
    title: 'Zero Tracks',
    subtitle: null,
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'test', external_id: 'ext-1', url: 'https://altune.test/a1' }],
    extras,
  };
}

// A locally-saved album: no provider source, which is the case that reaches this
// rail through "Your albums" (#1668).
function savedAlbum(title: string): DiscoveryResult {
  return {
    kind: 'album',
    title,
    subtitle: 'Prolific Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: { record_type: 'album', track_count: 3 },
  };
}

describe('DiscographySections(): a track_count of 0 is announced exactly as it is shown', () => {
  it('shows "0 tracks" and announces it in the accessibility label', () => {
    render(
      <DiscographySections albums={[album({ record_type: 'album', track_count: 0 })]} onAlbumPress={jest.fn()} />,
    );

    // The visible caption prints "0 tracks".
    expect(screen.getByText('0 tracks')).toBeTruthy();

    // The accessibility label must announce the same count it shows.
    const label = screen.getByTestId('detail-album-0').props.accessibilityLabel;
    expect(label).toContain('0 tracks');
  });
});

describe('DiscographySections(): "See all" on a discography longer than the rail window', () => {
  it('mounts a window of cards rather than one card per album', () => {
    const albums = Array.from({ length: 300 }, (_unused, i) => savedAlbum(`Album ${i}`));
    render(<DiscographySections albums={albums} onAlbumPress={jest.fn()} />);

    fireEvent.press(screen.getByTestId('detail-see-all-album'));

    // Expanding must not turn the whole list into one synchronous mount: the rail
    // renders its first window and reaches the rest as the user scrolls.
    const cards = screen.queryAllByTestId(/^detail-album-\d+$/);
    expect(cards.length).toBeGreaterThan(0);
    expect(cards.length).toBeLessThanOrEqual(10);
  });
});

function release(title: string, recordType: string): DiscoveryResult {
  return {
    kind: 'album',
    title,
    subtitle: null,
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: { record_type: recordType, track_count: 1 },
  };
}

function releases(count: number, recordType: string, prefix: string): DiscoveryResult[] {
  return Array.from({ length: count }, (_unused, i) => release(`${prefix} ${i}`, recordType));
}

describe('DiscographySections(): which record-type chips show', () => {
  it('renders nothing for an empty discography or one with no known record type', () => {
    const empty = render(<DiscographySections albums={[]} onAlbumPress={jest.fn()} />);
    expect(empty.toJSON()).toBeNull();

    const unknown = render(
      <DiscographySections albums={[release('X', 'bootleg'), release('Y', '')]} onAlbumPress={jest.fn()} />,
    );
    expect(unknown.toJSON()).toBeNull();
  });

  it('shows no chip bar when only one record type is present, just its rail', () => {
    render(<DiscographySections albums={[release('S1', 'single'), release('S2', 'single')]} onAlbumPress={jest.fn()} />);

    expect(screen.queryAllByTestId(/^detail-discography-(album|single|ep)$/)).toHaveLength(0);
    expect(screen.getByTestId('detail-single-0').props.accessibilityLabel).toBe('Singles: S1, 1 tracks');
  });

  it('shows albums, singles, EPs in that order whatever the input order, each announcing its count', () => {
    render(
      <DiscographySections
        albums={[release('E', 'ep'), release('S', 'single'), release('A1', 'album'), release('A2', 'album')]}
        onAlbumPress={jest.fn()}
      />,
    );

    const chips = screen.getAllByTestId(/^detail-discography-(album|single|ep)$/);
    expect(chips.map((chip) => chip.props.testID)).toEqual([
      'detail-discography-album',
      'detail-discography-single',
      'detail-discography-ep',
    ]);
    expect(chips.map((chip) => chip.props.accessibilityLabel)).toEqual(['Albums, 2', 'Singles, 1', 'EPs, 1']);
    expect(screen.getByTestId('detail-discography-album').props.accessibilityState).toEqual({ selected: true });
  });
});

describe('DiscographySections(): choosing a chip', () => {
  it('marks only the pressed chip selected and shows only its releases', () => {
    render(<DiscographySections albums={[release('A1', 'album'), release('E1', 'ep')]} onAlbumPress={jest.fn()} />);

    fireEvent.press(screen.getByTestId('detail-discography-ep'));

    expect(screen.getByTestId('detail-discography-ep').props.accessibilityState).toEqual({ selected: true });
    expect(screen.getByTestId('detail-discography-album').props.accessibilityState).toEqual({ selected: false });
    expect(screen.getByTestId('detail-ep-0').props.accessibilityLabel).toBe('EPs: E1, 1 tracks');
    expect(screen.queryByText('A1')).toBeNull();
  });

  it('falls back to the first present type when the chosen type leaves the discography', () => {
    const { rerender } = render(
      <DiscographySections albums={[release('A1', 'album'), release('S1', 'single')]} onAlbumPress={jest.fn()} />,
    );
    fireEvent.press(screen.getByTestId('detail-discography-single'));

    rerender(<DiscographySections albums={[release('A1', 'album'), release('E1', 'ep')]} onAlbumPress={jest.fn()} />);

    expect(screen.getByTestId('detail-discography-album').props.accessibilityState).toEqual({ selected: true });
    expect(screen.getByTestId('detail-discography-ep').props.accessibilityState).toEqual({ selected: false });
    expect(screen.getByTestId('detail-album-0').props.accessibilityLabel).toBe('Albums: A1, 1 tracks');
  });
});

describe('DiscographySections(): pressing a release', () => {
  it('hands the caller the pressed album, once', () => {
    const albums = [release('A1', 'album'), release('A2', 'album')];
    const onAlbumPress = jest.fn();
    render(<DiscographySections albums={albums} onAlbumPress={onAlbumPress} />);

    fireEvent.press(screen.getByTestId('detail-album-1'));

    expect(onAlbumPress).toHaveBeenCalledTimes(1);
    expect(onAlbumPress).toHaveBeenCalledWith(albums[1]);
  });

  it('hands the caller the pressed release from the filtered rail, not the unfiltered list', () => {
    const albums = [release('A1', 'album'), release('S1', 'single'), release('S2', 'single')];
    const onAlbumPress = jest.fn();
    render(<DiscographySections albums={albums} onAlbumPress={onAlbumPress} />);

    fireEvent.press(screen.getByTestId('detail-discography-single'));
    fireEvent.press(screen.getByTestId('detail-single-1'));

    expect(onAlbumPress).toHaveBeenCalledTimes(1);
    expect(onAlbumPress).toHaveBeenCalledWith(albums[2]);
  });
});

describe('DiscographySections(): the 10-release cap and "See all"', () => {
  it('offers "See all" at 11 releases of a type but not at exactly 10', () => {
    const { rerender } = render(<DiscographySections albums={releases(10, 'album', 'A')} onAlbumPress={jest.fn()} />);
    expect(screen.queryByTestId('detail-see-all-album')).toBeNull();

    rerender(<DiscographySections albums={releases(11, 'album', 'A')} onAlbumPress={jest.fn()} />);
    expect(screen.getByTestId('detail-see-all-album')).toBeTruthy();
  });

  it('collapses back to the cap after switching to another chip and returning', () => {
    render(
      <DiscographySections
        albums={[...releases(12, 'album', 'A'), ...releases(12, 'single', 'S')]}
        onAlbumPress={jest.fn()}
      />,
    );

    fireEvent.press(screen.getByTestId('detail-see-all-album'));
    expect(screen.queryByTestId('detail-see-all-album')).toBeNull();

    fireEvent.press(screen.getByTestId('detail-discography-single'));
    expect(screen.getByTestId('detail-see-all-single')).toBeTruthy();

    fireEvent.press(screen.getByTestId('detail-discography-album'));
    expect(screen.getByTestId('detail-see-all-album')).toBeTruthy();
  });

  it('collapses back to the cap when the already-selected chip is pressed again', () => {
    render(
      <DiscographySections albums={[...releases(12, 'album', 'A'), release('S', 'single')]} onAlbumPress={jest.fn()} />,
    );

    fireEvent.press(screen.getByTestId('detail-see-all-album'));
    fireEvent.press(screen.getByTestId('detail-discography-album'));

    expect(screen.getByTestId('detail-see-all-album')).toBeTruthy();
  });
});

describe('DiscographySections(): "See all" announces the true total, not the capped window', () => {
  it('says the full count of releases behind the cap, not the 10 shown', () => {
    render(<DiscographySections albums={releases(15, 'album', 'A')} onAlbumPress={jest.fn()} />);

    const label = screen.getByTestId('detail-see-all-album').props.accessibilityLabel;
    expect(label).toBe('See all 15 albums');
  });
});

let mockDiscographyWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockDiscographyWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

function measureDiscographyGrid(width: number) {
  fireEvent(screen.getByTestId('detail-discography-grid-measure'), 'layout', {
    nativeEvent: { layout: { width, height: 400, x: 0, y: 0 } },
  });
}

function renderedWidth(testID: string): unknown {
  return StyleSheet.flatten(screen.getByTestId(testID).props.style).width;
}

describe('DiscographySections(): on native at 1440px nothing changes', () => {
  afterEach(() => {
    Platform.OS = 'ios';
    mockDiscographyWindowWidth = 390;
  });

  it('keeps the Android rail with 128px cards at a 1440px window', () => {
    Platform.OS = 'android';
    mockDiscographyWindowWidth = 1440;
    render(<DiscographySections albums={releases(2, 'album', 'A')} onAlbumPress={jest.fn()} />);

    expect(screen.getByTestId('detail-discography-rail')).toBeTruthy();
    expect(screen.queryByTestId('detail-discography-grid')).toBeNull();
    expect(renderedWidth('detail-album-0')).toBe(128);
  });
});

describe('DiscographySections(): the grid on the web at 1440px', () => {
  beforeEach(() => {
    Platform.OS = 'web';
    mockDiscographyWindowWidth = 1440;
  });

  afterEach(() => {
    Platform.OS = 'ios';
    mockDiscographyWindowWidth = 390;
  });

  it('shows every card at a positive width before the grid has been measured', () => {
    render(<DiscographySections albums={releases(3, 'album', 'A')} onAlbumPress={jest.fn()} />);

    expect(screen.getAllByTestId(/^detail-album-\d+$/)).toHaveLength(3);
    expect(renderedWidth('detail-album-0')).toEqual(expect.any(Number));
    expect(renderedWidth('detail-album-0') as number).toBeGreaterThan(0);
  });

  it('sizes cards so 4 to 6 fit the measured width, and resizes them when it changes', () => {
    render(<DiscographySections albums={releases(8, 'album', 'A')} onAlbumPress={jest.fn()} />);

    measureDiscographyGrid(640);
    const at640 = renderedWidth('detail-album-0') as number;
    expect(at640 * 4).toBeLessThanOrEqual(640);
    expect(at640 * 7).toBeGreaterThan(640);

    measureDiscographyGrid(1200);
    const at1200 = renderedWidth('detail-album-0') as number;
    expect(at1200 * 4).toBeLessThanOrEqual(1200);
    expect(at1200 * 7).toBeGreaterThan(1200);
    expect(at1200).toBeGreaterThan(at640);
  });

  it('keeps the card width when only the window changes and the measured width does not', () => {
    const albums = releases(8, 'album', 'A');
    const { rerender } = render(<DiscographySections albums={albums} onAlbumPress={jest.fn()} />);
    measureDiscographyGrid(800);
    const before = renderedWidth('detail-album-0');

    mockDiscographyWindowWidth = 1920;
    rerender(<DiscographySections albums={albums} onAlbumPress={jest.fn()} />);

    expect(renderedWidth('detail-album-0')).toBe(before);
  });

  it('never gives a card a negative width when the grid measures zero or very narrow', () => {
    render(<DiscographySections albums={releases(8, 'album', 'A')} onAlbumPress={jest.fn()} />);

    measureDiscographyGrid(0);
    expect(renderedWidth('detail-album-0') as number).toBeGreaterThanOrEqual(0);

    measureDiscographyGrid(120);
    const narrow = renderedWidth('detail-album-0') as number;
    expect(narrow).toBeGreaterThanOrEqual(0);
    expect(narrow * 4).toBeLessThanOrEqual(120);
  });

  it('caps the grid at 10 with "See all 15 albums", then shows all 15 when pressed', () => {
    render(<DiscographySections albums={releases(15, 'album', 'A')} onAlbumPress={jest.fn()} />);
    measureDiscographyGrid(800);

    expect(screen.getByTestId('detail-see-all-album').props.accessibilityLabel).toBe('See all 15 albums');
    expect(screen.getByTestId('detail-album-9')).toBeTruthy();
    expect(screen.queryByTestId('detail-album-10')).toBeNull();

    fireEvent.press(screen.getByTestId('detail-see-all-album'));

    expect(screen.queryByTestId('detail-see-all-album')).toBeNull();
    expect(screen.getByTestId('detail-album-14')).toBeTruthy();
  });

  it('offers no "See all" in the grid at exactly 10 releases', () => {
    render(<DiscographySections albums={releases(10, 'album', 'A')} onAlbumPress={jest.fn()} />);
    measureDiscographyGrid(800);

    expect(screen.queryByTestId('detail-see-all-album')).toBeNull();
    expect(screen.getByTestId('detail-album-9')).toBeTruthy();
  });

  it('announces the singles total on "See all" after switching chips in the grid', () => {
    render(
      <DiscographySections
        albums={[...releases(12, 'album', 'A'), ...releases(13, 'single', 'S')]}
        onAlbumPress={jest.fn()}
      />,
    );
    measureDiscographyGrid(800);

    fireEvent.press(screen.getByTestId('detail-discography-single'));

    expect(screen.getByTestId('detail-see-all-single').props.accessibilityLabel).toBe('See all 13 singles');
  });

  it('swaps the grid to the chosen chip and opens the pressed release from it', () => {
    const albums = [release('A1', 'album'), release('A2', 'album'), release('E1', 'ep')];
    const onAlbumPress = jest.fn();
    render(<DiscographySections albums={albums} onAlbumPress={onAlbumPress} />);
    measureDiscographyGrid(800);

    fireEvent.press(screen.getByTestId('detail-discography-ep'));

    expect(screen.getByTestId('detail-discography-grid')).toBeTruthy();
    expect(screen.getByTestId('detail-ep-0').props.accessibilityLabel).toBe('EPs: E1, 1 tracks');
    expect(screen.queryByTestId('detail-album-0')).toBeNull();
    expect(screen.queryByTestId('detail-ep-1')).toBeNull();

    fireEvent.press(screen.getByTestId('detail-ep-0'));
    expect(onAlbumPress).toHaveBeenCalledTimes(1);
    expect(onAlbumPress).toHaveBeenCalledWith(albums[2]);
  });

  it('opens the pressed grid card, once', () => {
    const albums = releases(6, 'album', 'A');
    const onAlbumPress = jest.fn();
    render(<DiscographySections albums={albums} onAlbumPress={onAlbumPress} />);
    measureDiscographyGrid(800);

    fireEvent.press(screen.getByTestId('detail-album-5'));

    expect(onAlbumPress).toHaveBeenCalledTimes(1);
    expect(onAlbumPress).toHaveBeenCalledWith(albums[5]);
  });

  it('exposes each grid card to assistive tech as a named button', () => {
    render(<DiscographySections albums={[release('A1', 'album')]} onAlbumPress={jest.fn()} />);
    measureDiscographyGrid(800);

    expect(screen.getByRole('button', { name: 'Albums: A1, 1 tracks' })).toBe(
      screen.getByTestId('detail-album-0'),
    );
  });

  it('renders nothing for an empty discography', () => {
    const empty = render(<DiscographySections albums={[]} onAlbumPress={jest.fn()} />);

    expect(empty.toJSON()).toBeNull();
  });
});

import { Platform, StyleSheet } from 'react-native';
