import { createLinkEvents, type LinkingSource, type UrlEvent } from '@features/signIn';
import { setPlatformOS } from '@testing/setPlatformOS';

type UrlHandler = (event: UrlEvent) => void;

interface FakeLinking {
  linking: LinkingSource;
  deliver(url: string): void;
  subscriptions(): number;
}

function fakeLinking(launchUrl: string | null): FakeLinking {
  const handlers = new Set<UrlHandler>();
  return {
    linking: {
      getInitialURL: () => Promise.resolve(launchUrl),
      addEventListener: (_type, handler) => {
        handlers.add(handler);
        return { remove: () => handlers.delete(handler) };
      },
    },
    deliver: (url) => handlers.forEach((handler) => handler({ url })),
    subscriptions: () => handlers.size,
  };
}

describe('linkEvents on native', () => {
  beforeEach(() => {
    setPlatformOS('ios');
  });

  it('resolves the URL the app was launched with', async () => {
    const { linking } = fakeLinking('altune://auth/callback?code=abc');

    await expect(createLinkEvents(linking).initialUrl()).resolves.toBe(
      'altune://auth/callback?code=abc',
    );
  });

  it('hands each delivered url event to the listener as a bare string', () => {
    const { linking, deliver } = fakeLinking(null);
    const listener = jest.fn();

    createLinkEvents(linking).subscribeUrl(listener);
    deliver('altune://auth/callback?code=native-event');

    expect(listener).toHaveBeenCalledWith('altune://auth/callback?code=native-event');
  });

  it('returns an unsubscribe that removes the subscription', () => {
    const { linking, deliver, subscriptions } = fakeLinking(null);
    const listener = jest.fn();

    createLinkEvents(linking).subscribeUrl(listener)();
    deliver('altune://auth/callback?code=late');

    expect(subscriptions()).toBe(0);
    expect(listener).not.toHaveBeenCalled();
  });
});

describe('linkEvents on web', () => {
  afterEach(() => {
    setPlatformOS('ios');
  });

  it('reports no launch url and subscribes to nothing, leaving the page url to the route', async () => {
    setPlatformOS('web');
    const { linking, deliver, subscriptions } = fakeLinking('altune://auth/callback?code=abc');
    const listener = jest.fn();

    const events = createLinkEvents(linking);
    const stop = events.subscribeUrl(listener);
    deliver('altune://auth/callback?code=web');

    await expect(events.initialUrl()).resolves.toBeNull();
    expect(subscriptions()).toBe(0);
    expect(listener).not.toHaveBeenCalled();
    stop();
  });
});
