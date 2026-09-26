import { Platform } from 'react-native';
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
