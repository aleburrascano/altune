import {
  chainUncaughtErrorHandler,
  handlersOrNone,
  noGlobalErrorHandlers,
  runtimeErrorHandlers,
  type GlobalErrorHandlers,
} from '@/mobile';

type Handler = (error: unknown, isFatal?: boolean) => void;

type FakeGlobalHandlers = GlobalErrorHandlers & { current: Handler };

function fakeGlobalHandlers(previous: Handler): FakeGlobalHandlers {
  const handlers: FakeGlobalHandlers = {
    current: previous,
    getGlobalHandler: () => handlers.current,
    setGlobalHandler: (onUncaught: Handler) => {
      handlers.current = onUncaught;
    },
  };
  return handlers;
}

describe('chainUncaughtErrorHandler', () => {
  it('reports nothing and throws nothing when given the no-op handlers of a platform without any', () => {
    const onError = jest.fn<void, [unknown]>();

    chainUncaughtErrorHandler(noGlobalErrorHandlers)(onError);

    expect(() =>
      noGlobalErrorHandlers.getGlobalHandler()(new Error('web error'), true),
    ).not.toThrow();
    expect(onError).not.toHaveBeenCalled();
  });

  it('uses the handlers the runtime provides', () => {
    const runtime = fakeGlobalHandlers(jest.fn());

    expect(handlersOrNone(runtime)).toBe(runtime);
  });

  it('wires the native runtime handlers where the runtime has them', () => {
    expect(runtimeErrorHandlers()).not.toBe(noGlobalErrorHandlers);
    expect(runtimeErrorHandlers().getGlobalHandler()).toEqual(expect.any(Function));
  });

  it('falls back to the no-op handlers on a runtime that provides none', () => {
    expect(handlersOrNone(undefined)).toBe(noGlobalErrorHandlers);
  });

  it('passes an uncaught error to the listener, then to the handler it replaced', () => {
    const calls: string[] = [];
    const previous = jest.fn<void, [unknown, boolean?]>(() => {
      calls.push('previous');
    });
    const onError = jest.fn<void, [unknown]>(() => {
      calls.push('listener');
    });
    const handlers = fakeGlobalHandlers(previous);

    chainUncaughtErrorHandler(handlers)(onError);
    const error = new Error('native crash');
    handlers.current(error, true);

    expect(onError).toHaveBeenCalledWith(error);
    expect(previous).toHaveBeenCalledWith(error, true);
    expect(calls).toEqual(['listener', 'previous']);
  });
});
