import { Platform, StyleSheet } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { darkTheme } from '@shared/ui';

import { AlbumRail } from '../ui/AlbumRail';

let mockWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

beforeEach(() => {
  mockWindowWidth = 390;
});

afterEach(() => {
  Platform.OS = 'ios';
  mockWindowWidth = 390;
});

function album(title: string): DiscoveryResult {
  return {
    kind: 'album',
    title,
    subtitle: null,
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'test', external_id: title, url: `https://altune.test/${title}` }],
    extras: { record_type: 'album' },
  };
}

function renderRail(items: DiscoveryResult[] = [album('A1'), album('A2')]) {
  render(
    <AlbumRail
      items={items}
      total={items.length}
      hasMore={false}
      typeKey="album"
      typeLabel="Albums"
      onAlbumPress={jest.fn()}
      onSeeAll={jest.fn()}
    />,
  );
}

describe('AlbumRail: compact (native, or web at a narrow width)', () => {
  it('renders the horizontal rail off the web platform even at a wide window width', () => {
    mockWindowWidth = 1440;
    renderRail();

    expect(screen.getByTestId('detail-discography-rail')).toBeTruthy();
    expect(screen.queryByTestId('detail-discography-grid')).toBeNull();
  });

  it('carries no border on a native rail card', () => {
    mockWindowWidth = 1440;
    renderRail();

    const flattened = StyleSheet.flatten(screen.getByTestId('detail-album-0').props.style) as Record<
      string,
      unknown
    >;

    expect(flattened.borderWidth).toBeUndefined();
  });

  it('renders the horizontal rail on the web at a narrow width', () => {
    Platform.OS = 'web';
    mockWindowWidth = 360;
    renderRail();

    expect(screen.getByTestId('detail-discography-rail')).toBeTruthy();
    expect(screen.queryByTestId('detail-discography-grid')).toBeNull();
  });
});

describe('AlbumRail: grid on the web at a wide width', () => {
  beforeEach(() => {
    Platform.OS = 'web';
    mockWindowWidth = 1440;
  });

  it('sizes its columns from its own measured width, not the window width', () => {
    renderRail();

    const measure = screen.getByTestId('detail-discography-grid-measure');
    fireEvent(measure, 'layout', { nativeEvent: { layout: { width: 640, height: 400, x: 0, y: 0 } } });

    const grid = screen.UNSAFE_getByProps({ testID: 'detail-discography-grid' });
    expect(grid.props.numColumns).toBe(4);
  });

  it('picks up more columns when its measured width is wider than the window', () => {
    mockWindowWidth = 1024;
    renderRail();

    const measure = screen.getByTestId('detail-discography-grid-measure');
    fireEvent(measure, 'layout', { nativeEvent: { layout: { width: 1152, height: 400, x: 0, y: 0 } } });

    const grid = screen.UNSAFE_getByProps({ testID: 'detail-discography-grid' });
    expect(grid.props.numColumns).toBe(6);
  });

  it('gives a grid card the same hover and focus ring as the sidebar', () => {
    renderRail();

    const cards = screen
      .UNSAFE_getAllByProps({ testID: 'detail-album-0' })
      .filter((node) => typeof node.props.style === 'function');
    expect(cards).toHaveLength(1);
    const resolveStyle = cards[0]!.props.style as (state: {
      hovered?: boolean;
      focused?: boolean;
      pressed?: boolean;
    }) => unknown[];

    expect(resolveStyle({ hovered: true, focused: false, pressed: false })).toEqual(
      expect.arrayContaining([{ backgroundColor: darkTheme.color.surface2 }]),
    );
    expect(resolveStyle({ hovered: false, focused: true, pressed: false })).toEqual(
      expect.arrayContaining([{ borderColor: darkTheme.color.accent }]),
    );
    expect(resolveStyle({ hovered: false, focused: false, pressed: false })).toEqual(
      expect.arrayContaining([{ borderColor: 'transparent' }]),
    );
  });
});

describe('AlbumRail: crossing the wide breakpoint on the web', () => {
  beforeEach(() => {
    Platform.OS = 'web';
  });

  afterEach(() => {
    Platform.OS = 'ios';
  });

  it('is a rail at 999px and becomes a grid at 1000px', () => {
    mockWindowWidth = 999;
    const props = {
      items: [album('A1'), album('A2')],
      total: 2,
      hasMore: false,
      typeKey: 'album',
      typeLabel: 'Albums',
      onAlbumPress: jest.fn(),
      onSeeAll: jest.fn(),
    };
    const { rerender } = render(<AlbumRail {...props} />);
    expect(screen.getByTestId('detail-discography-rail')).toBeTruthy();
    expect(screen.queryByTestId('detail-discography-grid')).toBeNull();

    mockWindowWidth = 1000;
    rerender(<AlbumRail {...props} />);
    expect(screen.queryByTestId('detail-discography-rail')).toBeNull();
    expect(screen.getByTestId('detail-discography-grid')).toBeTruthy();
  });
});

describe('AlbumRail: grid edges on the web at a wide width', () => {
  beforeEach(() => {
    Platform.OS = 'web';
    mockWindowWidth = 1440;
  });

  afterEach(() => {
    Platform.OS = 'ios';
  });

  it('renders no card and no "See all" when it has no items', () => {
    render(
      <AlbumRail
        items={[]}
        total={0}
        hasMore={false}
        typeKey="album"
        typeLabel="Albums"
        onAlbumPress={jest.fn()}
        onSeeAll={jest.fn()}
      />,
    );

    expect(screen.queryAllByTestId(/^detail-album-\d+$/)).toHaveLength(0);
    expect(screen.queryByTestId('detail-see-all-album')).toBeNull();
  });

  it('announces the full total on the grid "See all" card and asks for more once when pressed', () => {
    const onSeeAll = jest.fn();
    const items = Array.from({ length: 10 }, (_unused, i) => album(`A${i}`));
    render(
      <AlbumRail
        items={items}
        total={25}
        hasMore
        typeKey="album"
        typeLabel="Albums"
        onAlbumPress={jest.fn()}
        onSeeAll={onSeeAll}
      />,
    );
    fireEvent(screen.getByTestId('detail-discography-grid-measure'), 'layout', {
      nativeEvent: { layout: { width: 800, height: 400, x: 0, y: 0 } },
    });

    const seeAll = screen.getByTestId('detail-see-all-album');
    expect(seeAll.props.accessibilityLabel).toBe('See all 25 albums');
    fireEvent.press(seeAll);
    expect(onSeeAll).toHaveBeenCalledTimes(1);
  });

  it('sizes the "See all" tile from the grid\'s computed card width, not a fixed size', () => {
    const items = Array.from({ length: 10 }, (_unused, i) => album(`A${i}`));
    render(
      <AlbumRail
        items={items}
        total={25}
        hasMore
        typeKey="album"
        typeLabel="Albums"
        onAlbumPress={jest.fn()}
        onSeeAll={jest.fn()}
      />,
    );
    fireEvent(screen.getByTestId('detail-discography-grid-measure'), 'layout', {
      nativeEvent: { layout: { width: 800, height: 400, x: 0, y: 0 } },
    });

    const cardWidth = (
      StyleSheet.flatten(screen.getByTestId('detail-album-0').props.style) as Record<string, unknown>
    ).width;
    const seeAllWidth = (
      StyleSheet.flatten(screen.getByTestId('detail-see-all-album').props.style) as Record<string, unknown>
    ).width;

    expect(seeAllWidth).toBe(cardWidth);
    expect(seeAllWidth).not.toBe(128);
  });
});
