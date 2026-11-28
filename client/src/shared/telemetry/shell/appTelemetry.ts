import { apiFetch } from '@shared/apiClient';
import { appVersion } from '@shared/device';
import { defaultFileStore } from '@shared/files';
import { randomUuid } from '@shared/id';
import { isAppActive, subscribeAppState } from '@shared/lifecycle';
import { warn } from '@shared/log';
import { onIdentityChange, onSignOut } from '@shared/session';

import { eventPath } from '@shared/telemetry/core/eventRoute';
import { createOutboxStore } from '@shared/telemetry/adapters/outboxStore';
import type { TelemetryClock, TelemetryDeps } from '@shared/telemetry/ports/telemetry';
import { createTelemetry } from '@shared/telemetry/shell/telemetry';

const systemDeps: TelemetryDeps = {
  clock: {
    now: () => Date.now(),
    tick: () => performance.now(),
    random: () => Math.random(),
    newId: () => randomUuid(),
  },
  send: async (event) => {
    await apiFetch(eventPath(event), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(event),
    });
  },
  store: createOutboxStore(defaultFileStore, { warn }),
  appState: { isActive: isAppActive, subscribe: subscribeAppState },
  session: { onSignOut, onIdentityChange },
  log: { warn },
  appVersion,
};

type AppTelemetryOverrides = Partial<Omit<TelemetryDeps, 'clock'>> & {
  clock?: Partial<TelemetryClock>;
};

export function createAppTelemetry(overrides: AppTelemetryOverrides = {}) {
  const { clock, ...rest } = overrides;
  return createTelemetry({ ...systemDeps, ...rest, clock: { ...systemDeps.clock, ...clock } });
}
