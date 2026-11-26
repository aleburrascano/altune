import {
  chainWindowUncaughtErrors,
  chainWindowUnhandledRejections,
  runtimeWindow,
  type WindowEvents,
} from '@/mobile';

type Listener = (event: never) => void;

type FakeWindow = WindowEvents & { fire(type: string, event: object): void };

function fakeWindow(): FakeWindow {
  const errorListeners: Listener[] = [];
  const rejectionListeners: Listener[] = [];
  const byType: Record<string, Listener[]> = {
    error: errorListeners,
    unhandledrejection: rejectionListeners,
  };
  return {
    addEventListener(type: string, listener: Listener) {
      byType[type]?.push(listener);
    },
    fire(type, event) {
      byType[type]?.forEach((listener) => listener(event as never));
    },
  };
}

describe('chainWindowUncaughtErrors', () => {
  it('reports the error a window error event carries', () => {
    const win = fakeWindow();
    const onError = jest.fn();
    const failure = new Error('script blew up');

    chainWindowUncaughtErrors(win)(onError);
    win.fire('error', { error: failure, message: 'script blew up' });

    expect(onError).toHaveBeenCalledWith(failure);
  });

  it('reports the message as an error when the event carries no error object', () => {
    const win = fakeWindow();
    const onError = jest.fn();

    chainWindowUncaughtErrors(win)(onError);
    win.fire('error', { message: 'Script error.' });

    expect(onError).toHaveBeenCalledWith(new Error('Script error.'));
  });

  it('does nothing where there is no window to listen on', () => {
    const onError = jest.fn();

    expect(() => chainWindowUncaughtErrors(undefined)(onError)).not.toThrow();
    expect(onError).not.toHaveBeenCalled();
  });
});

describe('chainWindowUnhandledRejections', () => {
  it('reports the reason of an unhandled rejection', () => {
    const win = fakeWindow();
    const onRejection = jest.fn();
    const reason = new Error('dropped promise');

    chainWindowUnhandledRejections(win)(onRejection);
    win.fire('unhandledrejection', { reason });

    expect(onRejection).toHaveBeenCalledWith(reason);
  });

  it('does not report window errors as rejections', () => {
    const win = fakeWindow();
    const onRejection = jest.fn();

    chainWindowUnhandledRejections(win)(onRejection);
    win.fire('error', { error: new Error('not a rejection') });

    expect(onRejection).not.toHaveBeenCalled();
  });

  it('does nothing where there is no window to listen on', () => {
    expect(() => chainWindowUnhandledRejections(undefined)(jest.fn())).not.toThrow();
  });
});

describe('runtimeWindow', () => {
  it('finds no window where the runtime has none that can listen', () => {
    expect(runtimeWindow()).toBeUndefined();
  });
});
