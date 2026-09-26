import { Text } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { DetailScaffold } from '../ui/DetailScaffold';

jest.mock('react-native-safe-area-context', () => ({
  useSafeAreaInsets: () => ({ top: 20, bottom: 0, left: 0, right: 0 }),
}));

import { Platform } from 'react-native';

let mockWideScaffoldWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWideScaffoldWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

function renderScaffold(overrides: Partial<Parameters<typeof DetailScaffold>[0]> = {}) {
  const onBack = jest.fn();
  const props = {
    title: 'Random Access Memories',
    artworkUrl: 'https://cdn.altune.test/rand.jpg',
    onBack,
    actions: <Text>actions-slot</Text>,
    facts: <Text>facts-slot</Text>,
    children: <Text>children-slot</Text>,
    ...overrides,
  };
  render(<DetailScaffold {...props} />);
  return { onBack };
}

describe('DetailScaffold', () => {
  it('renders the title in the banner', () => {
    renderScaffold();
    expect(screen.getByTestId('detail-banner-title')).toHaveTextContent(
      'Random Access Memories',
    );
  });

  it('renders the secondary node under the banner title', () => {
    renderScaffold({ secondary: <Text>2013</Text> });
    expect(screen.getByText('2013')).toBeTruthy();
  });

  it('calls onBack when the back button is pressed', () => {
    const { onBack } = renderScaffold();
    fireEvent.press(screen.getByTestId('detail-back'));
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it('renders the body in actions, then facts, then children order', () => {
    renderScaffold();
    const body = screen.getByTestId('detail-body');
    const texts = body.findAllByType(Text).map((node) => node.props.children);
    expect(texts).toEqual(['actions-slot', 'facts-slot', 'children-slot']);
  });

  it('has no menu button and no detail-menu testID when menuItems is absent', () => {
    renderScaffold();
    expect(screen.queryByTestId('detail-menu')).toBeNull();
  });

  it('opens the context menu on menu press and closes it on backdrop press', () => {
    const onPress = jest.fn();
    renderScaffold({ menuItems: [{ label: 'Share', onPress }] });

    expect(screen.queryByLabelText('Share')).toBeNull();

    fireEvent.press(screen.getByTestId('detail-menu'));
    expect(screen.getByLabelText('Share')).toBeTruthy();

    fireEvent.press(screen.getByLabelText('Close menu'));
    expect(screen.queryByLabelText('Share')).toBeNull();
  });

  it('runs the pressed menu item and closes the menu', () => {
    const onPress = jest.fn();
    renderScaffold({ menuItems: [{ label: 'Share', onPress }] });

    fireEvent.press(screen.getByTestId('detail-menu'));
    fireEvent.press(screen.getByLabelText('Share'));

    expect(onPress).toHaveBeenCalledTimes(1);
    expect(screen.queryByLabelText('Share')).toBeNull();
  });
});

describe('DetailScaffold keeps its behaviour through the split', () => {
  function artworkUris(): string[] {
    return screen.UNSAFE_root.findAll(
      (node) => typeof node.type === 'string' && node.props.source != null,
    ).map((node) => [node.props.source].flat()[0]?.uri);
  }

  it('shows the artwork in the hero when there is one', () => {
    renderScaffold({ artworkUrl: 'https://cdn.altune.test/rand.jpg' });
    expect(artworkUris()).toEqual(['https://cdn.altune.test/rand.jpg']);
  });

  it('still renders the banner title and the body when there is no artwork', () => {
    renderScaffold({ artworkUrl: null });
    expect(artworkUris()).toEqual([]);
    expect(screen.getByTestId('detail-banner-title')).toHaveTextContent('Random Access Memories');
    expect(screen.getByText('children-slot')).toBeTruthy();
  });

  it('keeps a very long title whole in the banner', () => {
    const longTitle = `${'Harder Better Faster Stronger '.repeat(12)}(Extended Remix)`;
    renderScaffold({ title: longTitle });
    expect(screen.getByTestId('detail-banner-title')).toHaveTextContent(longTitle);
  });

  it('keeps the top bar title hidden until the page scrolls', () => {
    renderScaffold();
    expect(screen.getAllByText('Random Access Memories')).toHaveLength(1);
    expect(
      screen.getAllByText('Random Access Memories', { includeHiddenElements: true }),
    ).toHaveLength(2);
  });

  it('renders actions then children when there are no facts', () => {
    renderScaffold({ facts: undefined });
    const body = screen.getByTestId('detail-body');
    const texts = body.findAllByType(Text).map((node) => node.props.children);
    expect(texts).toEqual(['actions-slot', 'children-slot']);
  });

  it('has no menu button when menuItems is empty', () => {
    renderScaffold({ menuItems: [] });
    expect(screen.queryByTestId('detail-menu')).toBeNull();
  });

  it('reopens the menu after it was closed', () => {
    renderScaffold({ menuItems: [{ label: 'Share', onPress: jest.fn() }] });

    fireEvent.press(screen.getByTestId('detail-menu'));
    fireEvent.press(screen.getByLabelText('Close menu'));
    fireEvent.press(screen.getByTestId('detail-menu'));

    expect(screen.getByLabelText('Share')).toBeTruthy();
  });

  it('opening and closing the menu never navigates back', () => {
    const { onBack } = renderScaffold({ menuItems: [{ label: 'Share', onPress: jest.fn() }] });

    fireEvent.press(screen.getByTestId('detail-menu'));
    fireEvent.press(screen.getByLabelText('Close menu'));

    expect(onBack).not.toHaveBeenCalled();
  });

  it('labels the back and menu buttons for screen readers', () => {
    renderScaffold({ menuItems: [{ label: 'Share', onPress: jest.fn() }] });
    expect(screen.getByRole('button', { name: 'Go back' })).toBe(screen.getByTestId('detail-back'));
    expect(screen.getByRole('button', { name: 'More options' })).toBe(
      screen.getByTestId('detail-menu'),
    );
  });
});

describe('DetailScaffold on the web at a wide width', () => {
  beforeEach(() => {
    Platform.OS = 'web';
    mockWideScaffoldWindowWidth = 1440;
  });

  afterEach(() => {
    Platform.OS = 'ios';
    mockWideScaffoldWindowWidth = 390;
  });

  it('moves the hero banner into the left column beside the actions', () => {
    renderScaffold();

    expect(screen.queryByTestId('detail-body')).toBeNull();
    const left = screen.getByTestId('detail-body-left');
    expect(left.findAllByType(Text).map((node) => node.props.children)).toEqual([
      'Random Access Memories',
      'actions-slot',
    ]);
    expect(screen.getByTestId('detail-banner-title')).toBeTruthy();
  });

  it('keeps facts and children in the right column', () => {
    renderScaffold();

    const right = screen.getByTestId('detail-body-right');
    expect(right.findAllByType(Text).map((node) => node.props.children)).toEqual([
      'facts-slot',
      'children-slot',
    ]);
  });
});

describe('DetailScaffold stays a single column on native even at a wide width', () => {
  it('keeps the compact detail-body layout when Platform.OS is not web', () => {
    mockWideScaffoldWindowWidth = 1440;
    renderScaffold();

    expect(screen.getByTestId('detail-body')).toBeTruthy();
    expect(screen.queryByTestId('detail-body-wide')).toBeNull();
  });
});

describe('DetailScaffold across the wide web breakpoint', () => {
  afterEach(() => {
    Platform.OS = 'ios';
    mockWideScaffoldWindowWidth = 390;
  });

  it('puts the banner back above a single column when the web window narrows from 1440px to 800px', () => {
    Platform.OS = 'web';
    mockWideScaffoldWindowWidth = 1440;
    const props = {
      title: 'Random Access Memories',
      artworkUrl: null,
      onBack: jest.fn(),
      actions: <Text>actions-slot</Text>,
      children: <Text>children-slot</Text>,
    };
    const { rerender } = render(<DetailScaffold {...props} />);
    expect(screen.getByTestId('detail-body-left')).toBeTruthy();

    mockWideScaffoldWindowWidth = 800;
    rerender(<DetailScaffold {...props} />);

    expect(screen.queryByTestId('detail-body-left')).toBeNull();
    expect(screen.getByTestId('detail-body')).toBeTruthy();
    expect(screen.getByTestId('detail-banner-title')).toHaveTextContent('Random Access Memories');
    expect(screen.getByText('children-slot')).toBeTruthy();
  });

  it('stays a single column on Android at 1440px', () => {
    Platform.OS = 'android';
    mockWideScaffoldWindowWidth = 1440;
    renderScaffold();

    expect(screen.getByTestId('detail-body')).toBeTruthy();
    expect(screen.queryByTestId('detail-body-left')).toBeNull();
  });
});

describe('DetailScaffold on the web at 1440px: what lands in each column', () => {
  beforeEach(() => {
    Platform.OS = 'web';
    mockWideScaffoldWindowWidth = 1440;
  });

  afterEach(() => {
    Platform.OS = 'ios';
    mockWideScaffoldWindowWidth = 390;
  });

  it('shows the artwork once, in the left column', () => {
    renderScaffold({ artworkUrl: 'https://cdn.altune.test/rand.jpg' });

    const withSource = (node: { type: unknown; props: { source?: unknown } }) =>
      typeof node.type === 'string' && node.props.source != null;
    const left = screen.getByTestId('detail-body-left');
    expect(left.findAll(withSource).map((node) => [node.props.source].flat()[0]?.uri)).toEqual([
      'https://cdn.altune.test/rand.jpg',
    ]);
    expect(screen.UNSAFE_root.findAll(withSource)).toHaveLength(1);
  });

  it('still splits into two columns with the title on the left when there is no artwork or facts', () => {
    renderScaffold({ artworkUrl: null, facts: undefined });

    const left = screen.getByTestId('detail-body-left');
    const right = screen.getByTestId('detail-body-right');
    expect(left.findAllByType(Text).map((node) => node.props.children)).toEqual([
      'Random Access Memories',
      'actions-slot',
    ]);
    expect(right.findAllByType(Text).map((node) => node.props.children)).toEqual(['children-slot']);
  });

  it('keeps back and the menu working in the two-column layout', () => {
    const onPress = jest.fn();
    const { onBack } = renderScaffold({ menuItems: [{ label: 'Share', onPress }] });

    fireEvent.press(screen.getByTestId('detail-back'));
    fireEvent.press(screen.getByTestId('detail-menu'));
    fireEvent.press(screen.getByLabelText('Share'));

    expect(onBack).toHaveBeenCalledTimes(1);
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('lays the discography out as a grid in the right column', () => {
    renderScaffold({
      children: (
        <DiscographySections
          albums={[1, 2, 3].map((n) => ({
            kind: 'album' as const,
            title: `A${n}`,
            subtitle: null,
            image_url: null,
            confidence: 'high' as const,
            sources: [],
            extras: { record_type: 'album', track_count: 1 },
          }))}
          onAlbumPress={jest.fn()}
        />
      ),
    });

    const right = screen.getByTestId('detail-body-right');
    expect(right.findAll((node) => node.props.testID === 'detail-discography-grid').length).toBeGreaterThan(0);
    expect(screen.queryByTestId('detail-discography-rail')).toBeNull();
  });
});

import { DiscographySections } from '../ui/DiscographySections';
