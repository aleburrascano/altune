import { renderHook } from '@testing-library/react-native';

import { useScreenViews } from '@shared/events';

interface Screen {
  route: string;
  signedIn: boolean;
}

function mount(first: Screen) {
  const sent: string[] = [];
  function onScreenView(route: string): void {
    sent.push(route);
  }
  const hook = renderHook(
    ({ route, signedIn }: Screen) => useScreenViews(route, signedIn, onScreenView),
    { initialProps: first },
  );
  return { sent, rerender: hook.rerender };
}

describe('useScreenViews()', () => {
  it('sends one screen view per route change', () => {
    const { sent, rerender } = mount({ route: '/(tabs)/home', signedIn: true });
    expect(sent).toEqual(['/(tabs)/home']);

    rerender({ route: '/(tabs)/library/playlist/[id]', signedIn: true });
    expect(sent).toEqual(['/(tabs)/home', '/(tabs)/library/playlist/[id]']);
  });

  it('sends nothing when the same route renders again', () => {
    const { sent, rerender } = mount({ route: '/(tabs)/home', signedIn: true });

    rerender({ route: '/(tabs)/home', signedIn: true });

    expect(sent).toEqual(['/(tabs)/home']);
  });

  it('sends nothing while signed out', () => {
    const { sent } = mount({ route: '/(tabs)/home', signedIn: false });

    expect(sent).toEqual([]);
  });

  it('sends the current route once the user signs in', () => {
    const { sent, rerender } = mount({ route: '/(tabs)/home', signedIn: false });

    rerender({ route: '/(tabs)/home', signedIn: true });

    expect(sent).toEqual(['/(tabs)/home']);
  });
});
