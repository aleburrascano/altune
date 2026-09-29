import { isAppActive, subscribeAppState } from '../appState';

type Handler = (state: string) => void;

const mockAppState = {
  currentState: 'active',
  handlers: [] as Handler[],
};

jest.mock('react-native/Libraries/AppState/AppState', () => ({
  default: {
    get currentState() {
      return mockAppState.currentState;
    },
    addEventListener: (_type: string, handler: Handler) => {
      mockAppState.handlers.push(handler);
      return {
        remove: () => {
          mockAppState.handlers = mockAppState.handlers.filter((h) => h !== handler);
        },
      };
    },
  },
}));

function emit(state: string): void {
  [...mockAppState.handlers].forEach((handler) => handler(state));
}

describe('appState', () => {
  beforeEach(() => {
    mockAppState.handlers = [];
    mockAppState.currentState = 'active';
  });

  it('calls the listener on each app state change', () => {
    const listener = jest.fn();
    subscribeAppState(listener);
    emit('background');
    emit('active');
    expect(listener.mock.calls).toEqual([['background'], ['active']]);
  });

  it('stops calling the listener once unsubscribed', () => {
    const listener = jest.fn();
    const unsubscribe = subscribeAppState(listener);
    unsubscribe();
    emit('background');
    expect(listener).not.toHaveBeenCalled();
  });

  it('reports whether the app is active from the current state', () => {
    expect(isAppActive()).toBe(true);
    mockAppState.currentState = 'background';
    expect(isAppActive()).toBe(false);
  });
});
