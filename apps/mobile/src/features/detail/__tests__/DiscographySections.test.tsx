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
