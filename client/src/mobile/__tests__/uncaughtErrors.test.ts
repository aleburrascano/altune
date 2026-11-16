import { chainUncaughtErrorHandler, type GlobalErrorHandlers } from '@/mobile';

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
