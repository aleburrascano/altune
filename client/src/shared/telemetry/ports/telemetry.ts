import type { DiscoveryEvent } from '@shared/telemetry/core/discoveryEvent';
import type { OutboxEntry } from '@shared/telemetry/core/outboxEntries';

export interface TelemetryClock {
  now: () => number;
  tick: () => number;
  random: () => number;
  newId: () => string;
}

export type SendEvent = (event: DiscoveryEvent) => Promise<void>;

export interface OutboxStore {
  load: () => OutboxEntry[];
  save: (entries: readonly OutboxEntry[]) => void;
}

export interface TelemetryAppState {
  isActive: () => boolean;
  subscribe: (listener: (status: string) => void) => void;
}

export interface TelemetryLog {
  warn: (message: string, ...details: unknown[]) => void;
}

export interface TelemetrySession {
  onSignOut: (listener: () => void) => void;
  onIdentityChange: (listener: (userId: string | null) => void) => () => void;
}

export interface TelemetryDeps {
  clock: TelemetryClock;
  send: SendEvent;
  store: OutboxStore;
  appState: TelemetryAppState;
  session: TelemetrySession;
  log: TelemetryLog;
  appVersion: () => string;
}
