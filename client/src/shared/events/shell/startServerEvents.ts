import { apiBase } from '@shared/apiConfig';
import { onSignOut } from '@shared/session';

import type { EventCache } from '@shared/events/core/eventCache';
import type { ServerEvent } from '@shared/events/core/sseFrames';
import { applyServerEvent } from '@shared/events/shell/applyServerEvent';
import type { SSEClient } from '@shared/events/shell/sseClient';

export type ServerEventsClient = Pick<SSEClient, 'connect' | 'disconnect' | 'dispose'>;

export type ServerEventsClientFactory = (
  url: string,
  getToken: () => Promise<string | null>,
  onEvent: (event: ServerEvent) => void,
  onError: (error: unknown) => void,
) => ServerEventsClient;

export interface AppForeground {
  isActive(): boolean;
  subscribe(listener: (state: string) => void): () => void;
}

export interface ServerEventsDeps {
  readonly cache: EventCache;
  readonly createClient: ServerEventsClientFactory;
  readonly getToken: () => Promise<string | null>;
  readonly foreground: AppForeground;
  readonly warn: (message: string, detail: unknown) => void;
}

function warnUnhandled(deps: ServerEventsDeps, event: ServerEvent): void {
  if (!applyServerEvent(deps.cache, event)) {
    deps.warn('[sse] unrecognized event type', { type: event.type });
  }
}

function openClient(deps: ServerEventsDeps): ServerEventsClient {
  return deps.createClient(
    `${apiBase()}/v1/events`,
    () => deps.getToken().catch(() => null),
    (event) => warnUnhandled(deps, event),
    (error) => deps.warn('[sse]', error),
  );
}

class ServerEventStream {
  private readonly deps: ServerEventsDeps;
  private readonly foreground: AppForeground;
  private client: ServerEventsClient;

  constructor(deps: ServerEventsDeps) {
    this.deps = deps;
    this.foreground = deps.foreground;
    this.client = openClient(deps);
  }

  connect(): void {
    void this.client.connect();
  }

  followForeground(state: string): void {
    if (state === 'active') this.connect();
    else this.client.disconnect();
  }

  reopen(): void {
    this.client.dispose();
    this.client = openClient(this.deps);
    this.connectInForeground();
  }

  dispose(): void {
    this.client.dispose();
  }

  private connectInForeground(): void {
    if (this.foreground.isActive()) this.connect();
  }
}

function follow(stream: ServerEventStream, foreground: AppForeground): (() => void)[] {
  return [
    foreground.subscribe((state) => stream.followForeground(state)),
    onSignOut(() => stream.reopen()),
  ];
}

export function startServerEvents(deps: ServerEventsDeps): () => void {
  const stream = new ServerEventStream(deps);
  stream.connect();
  const unsubscribes = follow(stream, deps.foreground);
  return () => {
    for (const unsubscribe of unsubscribes) unsubscribe();
    stream.dispose();
  };
}
