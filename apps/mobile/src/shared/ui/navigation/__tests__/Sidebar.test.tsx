import { Text } from 'react-native';
import { fireEvent, render, screen, within } from '@testing-library/react-native';

import { Sidebar } from '../Sidebar';
import type { TabRoute } from '../tabRoutes';

describe('Sidebar', () => {
  it('lists Discover, Library and Settings, in that order', () => {
    render(<Sidebar activeRoute="discover" onNavigate={jest.fn()} />);

    expect(screen.getByText('Discover')).toBeTruthy();
    expect(screen.getByText('Library')).toBeTruthy();
    expect(screen.getByText('Settings')).toBeTruthy();
  });

  it('marks the active route selected and leaves the rest unselected', () => {
    render(<Sidebar activeRoute="library" onNavigate={jest.fn()} />);

    expect(
      (screen.getByTestId('sidebar-item-library').props.accessibilityState as { selected: boolean })
        .selected,
    ).toBe(true);
    expect(
      (screen.getByTestId('sidebar-item-discover').props.accessibilityState as { selected: boolean })
        .selected,
    ).toBe(false);
  });

  it('navigates to the route of the item pressed', () => {
    const onNavigate = jest.fn<void, [TabRoute]>();
    render(<Sidebar activeRoute="discover" onNavigate={onNavigate} />);

    fireEvent.press(screen.getByTestId('sidebar-item-settings'));

    expect(onNavigate).toHaveBeenCalledWith('settings');
  });

  it('renders the playlists slot under Library, and nowhere else', () => {
    render(
      <Sidebar
        activeRoute="discover"
        onNavigate={jest.fn()}
        playlists={<Text testID="stub-playlists">Stub playlists</Text>}
      />,
    );

    expect(
      within(screen.getByTestId('sidebar-route-library')).getByTestId('stub-playlists').props
        .children,
    ).toBe('Stub playlists');
    expect(
      within(screen.getByTestId('sidebar-route-discover')).queryByTestId('sidebar-playlists'),
    ).toBeNull();
    expect(
      within(screen.getByTestId('sidebar-route-settings')).queryByTestId('sidebar-playlists'),
    ).toBeNull();
  });
});
