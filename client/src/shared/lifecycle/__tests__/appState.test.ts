import {
  driveAppState,
  isAppActive,
  setAppStateSource,
  subscribeAppState,
  type AppLifecycleState,
} from '@shared/lifecycle';

describe('appState', () => {
  it('calls the listener on each app state change', () => {
    const app = driveAppState();
    const listener = jest.fn();
    subscribeAppState(listener);

    app.change('background');
    app.change('active');

    expect(listener.mock.calls).toEqual([['background'], ['active']]);
  });

  it('stops calling the listener once unsubscribed', () => {
    const app = driveAppState();
    const listener = jest.fn();
    const unsubscribe = subscribeAppState(listener);

    unsubscribe();
    app.change('background');

    expect(listener).not.toHaveBeenCalled();
  });

  it('keeps calling the other listeners when one unsubscribes', () => {
    const app = driveAppState();
    const leaving = jest.fn();
    const staying = jest.fn();
    const unsubscribe = subscribeAppState(leaving);
    subscribeAppState(staying);

    unsubscribe();
    app.change('inactive');

    expect(leaving).not.toHaveBeenCalled();
    expect(staying).toHaveBeenCalledWith('inactive');
  });

  it('reports whether the app is active from the current state', () => {
    const app = driveAppState('active');
    expect(isAppActive()).toBe(true);

    app.change('background');

    expect(isAppActive()).toBe(false);
  });

  it('starts from the state the driver was given', () => {
    driveAppState('background');
    expect(isAppActive()).toBe(false);
  });

  it('keeps a listener that subscribed before the source changed', () => {
    const listener = jest.fn();
    subscribeAppState(listener);

    const app = driveAppState();
    app.change('background');

    expect(listener).toHaveBeenCalledWith('background');
  });

  it('stops hearing a source once another replaces it', () => {
    const listener = jest.fn();
    subscribeAppState(listener);
    const replaced = driveAppState();

    driveAppState();
    replaced.change('background');

    expect(listener).not.toHaveBeenCalled();
  });

  it('hands every change of a source the app sets to each listener', () => {
    const published: AppLifecycleState[] = [];
    const publishers: ((state: AppLifecycleState) => void)[] = [];
    setAppStateSource({
      current: () => 'unknown',
      onChange: (publish) => {
        publishers.push(publish);
        return () => undefined;
      },
    });

    subscribeAppState((state) => published.push(state));
    for (const publish of publishers) publish('extension');

    expect(published).toEqual(['extension']);
    expect(isAppActive()).toBe(false);
  });
});
