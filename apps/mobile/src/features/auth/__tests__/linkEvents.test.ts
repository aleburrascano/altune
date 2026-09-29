import * as Linking from 'expo-linking';

import { initialUrl, subscribeUrl } from '../native/linkEvents';

jest.mock('expo-linking', () => ({
  getInitialURL: jest.fn(),
  addEventListener: jest.fn(),
}));

const getInitialURL = Linking.getInitialURL as unknown as jest.Mock;
const addEventListener = Linking.addEventListener as unknown as jest.Mock;

describe('linkEvents', () => {
  beforeEach(() => {
    getInitialURL.mockReset();
    addEventListener.mockReset();
  });

  it('resolves the URL the app was launched with', async () => {
    getInitialURL.mockResolvedValue('altune://auth/callback?code=abc');

    await expect(initialUrl()).resolves.toBe('altune://auth/callback?code=abc');
  });

  it('hands each delivered url event to the listener as a bare string', () => {
    addEventListener.mockReturnValue({ remove: jest.fn() });
    const listener = jest.fn();

    subscribeUrl(listener);
    const [event, handler] = addEventListener.mock.calls[0] as [
      string,
      (e: { url: string }) => void,
    ];
    handler({ url: 'altune://auth/callback?code=native-event' });

    expect(event).toBe('url');
    expect(listener).toHaveBeenCalledWith('altune://auth/callback?code=native-event');
  });

  it('returns an unsubscribe that removes the subscription', () => {
    const remove = jest.fn();
    addEventListener.mockReturnValue({ remove });

    subscribeUrl(jest.fn())();

    expect(remove).toHaveBeenCalledTimes(1);
  });
});
