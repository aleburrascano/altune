import type { TelemetryDeps } from '@shared/telemetry/ports/telemetry';
import { createClientErrorReporter } from '@shared/telemetry/shell/clientErrorReporting';
import { createEventSession } from '@shared/telemetry/shell/eventSession';
import { createOutbox } from '@shared/telemetry/shell/outbox';
import { createRecordEvent } from '@shared/telemetry/shell/recordEvent';
import { createUserTelemetry } from '@shared/telemetry/shell/userTelemetry';

export function createTelemetry(deps: TelemetryDeps) {
  const { clock, appState, session, log, store, send, appVersion } = deps;
  const eventSession = createEventSession({ clock, appState, onSignOut: session.onSignOut });
  const recordEvent = createRecordEvent(send, eventSession.getSessionId);
  const outbox = createOutbox({
    clock,
    store,
    appState,
    log,
    recordEvent,
    onSignOut: session.onSignOut,
  });
  const errors = createClientErrorReporter({
    clock,
    appVersion,
    enqueueCritical: outbox.enqueueCritical,
  });
  const user = createUserTelemetry({ recordEvent, log });
  const unfollowers: (() => void)[] = [];

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
    dispose: (): void => {
      unfollowers.splice(0).forEach((unfollow) => unfollow());
      outbox.dispose();
      eventSession.dispose();
      errors.dispose();
    },
    followSignedInUser: (): (() => void) => {
      const unfollow = session.onIdentityChange(outbox.setOwner);
      unfollowers.push(unfollow);
      return unfollow;
    },
  };
}

export type Telemetry = ReturnType<typeof createTelemetry>;
