import {
  chainUncaughtErrorHandler,
  globalErrorHandlersFor,
  noGlobalErrorHandlers,
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

    chainUncaughtErrorHandler(onError, noGlobalErrorHandlers);

    expect(() =>
      noGlobalErrorHandlers.getGlobalHandler()(new Error('web error'), true),
    ).not.toThrow();
    expect(onError).not.toHaveBeenCalled();
  });

  it('wires the no-op handlers on web and the runtime handlers on native', () => {
    expect(globalErrorHandlersFor('web')).toBe(noGlobalErrorHandlers);
    expect(globalErrorHandlersFor('ios')).not.toBe(noGlobalErrorHandlers);
    expect(globalErrorHandlersFor('android')).not.toBe(noGlobalErrorHandlers);
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

    chainUncaughtErrorHandler(onError, handlers);
    const error = new Error('native crash');
    handlers.current(error, true);

    expect(onError).toHaveBeenCalledWith(error);
    expect(previous).toHaveBeenCalledWith(error, true);
    expect(calls).toEqual(['listener', 'previous']);
  });
});
