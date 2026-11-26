import { createAppTelemetry, createOutboxStore, type DiscoveryEvent } from '@shared/telemetry';
import { createMemoryFileStore } from '@testing/fakes/files/inMemoryFileStore';

type Gate = { promise: Promise<void>; fail: (error: Error) => void };

function neverFails(): void {
  return undefined;
}

function gate(): Gate {
  let fail: (error: Error) => void = neverFails;
  const promise = new Promise<void>((_resolve, reject) => {
    fail = reject;
  });
  return { promise, fail };
}

function settle(): Promise<void> {
  return new Promise((resolve) => setImmediate(resolve));
}

const LIBRARY_ADD: DiscoveryEvent = { type: 'library_add' };

function bootWith(send: (event: DiscoveryEvent) => Promise<void>) {
  const warn = jest.fn();
  const store = createOutboxStore(createMemoryFileStore(), { warn });
  const telemetry = createAppTelemetry({ store, send, log: { warn } });
  return { telemetry, warn };
}

describe('stopping telemetry', () => {
  afterEach(() => {
    jest.useRealTimers();
  });

  it('waits for the flush in flight, then logs nothing and leaves no retry timer', async () => {
    jest.useFakeTimers({ doNotFake: ['setImmediate'] });
    const reply = gate();
    const { telemetry, warn } = bootWith(() => reply.promise);
    const queued = telemetry.enqueueCritical(LIBRARY_ADD);
    await settle();

    let stopped = false;
    const stopping = telemetry.stop().then(() => {
      stopped = true;
    });
    await settle();
    expect(stopped).toBe(false);

    reply.fail(new Error('unreachable'));
    await stopping;
    await queued;

    expect(warn).not.toHaveBeenCalled();
    expect(jest.getTimerCount()).toBe(0);
  });

  it('logs nothing for a user action that fails after the stop', async () => {
    const reply = gate();
    const { telemetry, warn } = bootWith(() => reply.promise);
    telemetry.recordUserAction({ action: 'play', outcome: 'tapped' });
    const stopping = telemetry.stop();

    reply.fail(new Error('unreachable'));
    await stopping;
    await settle();

    expect(warn).not.toHaveBeenCalled();
  });

  it('sends nothing recorded after the stop', async () => {
    const send = jest.fn(() => Promise.resolve());
    const { telemetry } = bootWith(send);
    await telemetry.stop();

    await telemetry.recordEvent(LIBRARY_ADD);
    telemetry.recordUserAction({ action: 'play', outcome: 'tapped' });
    await telemetry.enqueueCritical(LIBRARY_ADD);
    await settle();

    expect(send).not.toHaveBeenCalled();
  });

  it('keeps logging a failed user action while it is running', async () => {
    const { telemetry, warn } = bootWith(() => Promise.reject(new Error('unreachable')));

    telemetry.recordUserAction({ action: 'play', outcome: 'tapped' });
    await settle();

    expect(warn).toHaveBeenCalledWith('[telemetry] recording event failed', expect.any(Error));
    telemetry.dispose();
  });
});
