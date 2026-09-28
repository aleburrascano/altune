import { render } from '@testing-library/react-native';

import { pageTitleFor } from '../../shared/ui/navigation/pageTitle';
import * as base from '../platformExtras';
import * as android from '../platformExtras.android';
import * as web from '../platformExtras.web';

const mockUseKeyboardShortcuts = jest.fn();
const mockPlayback = { marker: 'playback' };
const mockNavigationBar = jest.fn();

jest.mock('expo-navigation-bar', () => ({
  NavigationBar: (props: { style: string }) => {
    mockNavigationBar(props);
    return null;
  },
}));

jest.mock('expo-router/head', () => {
  const { View } = jest.requireActual('react-native');
  return {
    __esModule: true,
    default: ({ children }: { children: unknown }) => <View>{children}</View>,
  };
});

jest.mock('../../shared/playback/usePlayback', () => ({ usePlayback: () => mockPlayback }));
jest.mock('../../shared/ui/keyboard/useKeyboardShortcuts', () => ({
  useKeyboardShortcuts: (...args: unknown[]) => mockUseKeyboardShortcuts(...args),
}));
jest.mock('../../features/library/hooks/usePlaylistActions', () => ({
  usePlaylistActions: () => ({ playlists: [{ id: 'p1', name: 'Road Trip' }] }),
}));

beforeEach(() => {
  mockUseKeyboardShortcuts.mockClear();
  mockNavigationBar.mockClear();
});

describe('platformExtras default', () => {
  it('renders nothing for all three components', () => {
    const { toJSON } = render(
      <>
        <base.SystemNavigationBar scheme="dark" />
        <base.PlaybackShortcuts />
        <base.TabsTitle pathname="/library" />
      </>,
    );
    expect(toJSON()).toBeNull();
    expect(mockNavigationBar).not.toHaveBeenCalled();
    expect(mockUseKeyboardShortcuts).not.toHaveBeenCalled();
  });
});

describe('platformExtras.android', () => {
  it('renders the NavigationBar with a light style on a dark scheme', () => {
    render(<android.SystemNavigationBar scheme="dark" />);
    expect(mockNavigationBar).toHaveBeenCalledWith({ style: 'light' });
  });

  it('renders the NavigationBar with a dark style on a light scheme', () => {
    render(<android.SystemNavigationBar scheme="light" />);
    expect(mockNavigationBar).toHaveBeenCalledWith({ style: 'dark' });
  });

  it('renders nothing for shortcuts and title', () => {
    const { toJSON } = render(
      <>
        <android.PlaybackShortcuts />
        <android.TabsTitle pathname="/library" />
      </>,
    );
    expect(toJSON()).toBeNull();
  });
});

describe('platformExtras.web', () => {
  it('wires useKeyboardShortcuts to usePlayback', () => {
    render(<web.PlaybackShortcuts />);
    expect(mockUseKeyboardShortcuts).toHaveBeenCalledWith(mockPlayback);
  });

  it('renders the page title for the pathname', () => {
    const { toJSON } = render(<web.TabsTitle pathname="/discover" />);
    expect(JSON.stringify(toJSON())).toContain(pageTitleFor('/discover'));
  });

  it('renders the playlist name in the title on a playlist page', () => {
    const { toJSON } = render(<web.TabsTitle pathname="/library/playlist/p1" />);
    expect(JSON.stringify(toJSON())).toContain(pageTitleFor('/library/playlist/p1', 'Road Trip'));
  });

  it('renders nothing for the navigation bar', () => {
    const { toJSON } = render(<web.SystemNavigationBar scheme="dark" />);
    expect(toJSON()).toBeNull();
  });
});
