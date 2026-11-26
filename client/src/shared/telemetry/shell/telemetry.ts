import type { TelemetryDeps, TelemetryLog } from '@shared/telemetry/ports/telemetry';
import { createClientErrorReporter } from '@shared/telemetry/shell/clientErrorReporting';
import { createEventSession } from '@shared/telemetry/shell/eventSession';
import { createOutbox } from '@shared/telemetry/shell/outbox';
import { createRecordEvent, type RecordEvent } from '@shared/telemetry/shell/recordEvent';
import { createUserTelemetry } from '@shared/telemetry/shell/userTelemetry';

export function createTelemetry(deps: TelemetryDeps) {
  const { clock, appState, session, log, store, send, appVersion } = deps;
  let stopped = false;
  const sending = new Set<Promise<void>>();
  const liveLog: TelemetryLog = {
    warn: (message, ...details) => {
      if (!stopped) log.warn(message, ...details);
    },
  };
  const eventSession = createEventSession({ clock, appState, onSignOut: session.onSignOut });
  const sendRecorded = createRecordEvent(send, eventSession.getSessionId);
  function recordEvent(...args: Parameters<RecordEvent>): ReturnType<RecordEvent> {
    if (stopped) return Promise.resolve();
    const attempt = sendRecorded(...args);
    sending.add(attempt);
    function done(): void {
      sending.delete(attempt);
    }
    attempt.then(done, done);
    return attempt;
  }
  const outbox = createOutbox({
    clock,
    store,
    appState,
    log: liveLog,
    recordEvent,
    onSignOut: session.onSignOut,
  });
  const errors = createClientErrorReporter({
    clock,
    appVersion,
    enqueueCritical: outbox.enqueueCritical,
  });
  const user = createUserTelemetry({ recordEvent, log: liveLog });
  const unfollowers: (() => void)[] = [];

  function dispose(): void {
    stopped = true;
    unfollowers.splice(0).forEach((unfollow) => unfollow());
    outbox.dispose();
    eventSession.dispose();
    errors.dispose();
  }

  async function stop(): Promise<void> {
    dispose();
    await Promise.allSettled([...sending, outbox.settled()]);
  }

  return {
    recordEvent,
    getSessionId: eventSession.getSessionId,
    enqueueCritical: outbox.enqueueCritical,
    flushOutbox: outbox.flushOutbox,
    reportClientError: errors.reportClientError,
    installGlobalErrorReporting: errors.installGlobalErrorReporting,
    recordUserAction: user.recordUserAction,
    recordFailureShown: user.recordFailureShown,
    setOutboxOwner: outbox.setOwner,
    dispose,
    stop,
    followSignedInUser: (): (() => void) => {
      const unfollow = session.onIdentityChange(outbox.setOwner);
      unfollowers.push(unfollow);
      return unfollow;
    },
  };
}

export type Telemetry = ReturnType<typeof createTelemetry>;
