import { CORRELATION_HEADER, newCorrelationId } from '@shared/api-client/correlationId';
import { markSessionExpired, stampCredentials } from '@shared/auth/sessionExpired';
import { HTTP_UNAUTHORIZED } from '@shared/errors';
import { isServerEvent, parseChunk } from '@shared/events/sseFrames';
import type { ServerEvent } from '@shared/events/sseFrames';
import { recordEvent } from '@shared/telemetry/recordEvent';

export const HEARTBEAT_WATCHDOG_MS = 60_000;
export const MAX_RESPONSE_BYTES = 512 * 1024;

const BASE_RECONNECT_MS = 1_000;
const MAX_RECONNECT_MS = 30_000;
const MAX_RETRY_AFTER_MS = 5 * 60_000;

function reconnectDelayMs(attempt: number, minimumDelayMs: number): number {
  const base = Math.min(BASE_RECONNECT_MS * 2 ** attempt, MAX_RECONNECT_MS);
  return Math.max(base + Math.random() * base, minimumDelayMs);
}

export class ServerEventHandlerError extends Error {
  readonly eventId: string;
  readonly eventType: string;

  constructor(eventId: string, eventType: string, cause: unknown) {
    super(`SSE event handler threw (type=${eventType}, id=${eventId || '<none>'})`, { cause });
    this.name = 'ServerEventHandlerError';
    this.eventId = eventId;
    this.eventType = eventType;
  }
}

class SSEConnectionError extends Error {
  readonly correlationId: string | null;

  constructor(correlationId: string | null) {
    super(
      correlationId === null
        ? 'SSE connection error'
        : `SSE connection error (correlation_id=${correlationId})`,
    );
    this.name = 'SSEConnectionError';
    this.correlationId = correlationId;
  }
}

export class SSEHttpError extends Error {
  readonly status: number;
  readonly correlationId: string | null;

  constructor(status: number, correlationId: string | null) {
    super(
      correlationId === null
        ? `SSE stream refused (status=${status})`
        : `SSE stream refused (status=${status}, correlation_id=${correlationId})`,
    );
    this.name = 'SSEHttpError';
    this.status = status;
    this.correlationId = correlationId;
  }
}

function isRefusal(status: number): boolean {
  return status >= 300;
}

function retryAfterMs(header: string | null, now: number): number {
  if (header === null) return 0;
  const value = header.trim();
  const wait = /^\d+$/.test(value) ? Number(value) * 1_000 : Date.parse(value) - now;
  if (Number.isNaN(wait)) return 0;
  return Math.min(Math.max(wait, 0), MAX_RETRY_AFTER_MS);
}

type EventHandler = (event: ServerEvent) => void;
type ErrorHandler = (error: unknown) => void;

export class SSEClient {
  private xhr: XMLHttpRequest | null = null;
  private lastEventId = '';
  private processedLength = 0;
  private buffer = '';
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectAttempt = 0;
  private episodeReported = false;
  private stalledCaps = 0;
  private lastEventIdAtOpen = '';
  private watchdogTimer: ReturnType<typeof setTimeout> | null = null;
  private disposed = false;
  private connecting = false;
  private connectionRequested = false;

  private url: string;
  private getToken: () => Promise<string | null>;
  private onEvent: EventHandler;
  private onError: ErrorHandler;

  constructor(
    url: string,
    getToken: () => Promise<string | null>,
    onEvent: EventHandler,
    onError: ErrorHandler,
  ) {
    this.url = url;
    this.getToken = getToken;
    this.onEvent = onEvent;
    this.onError = onError;
  }

  async connect(): Promise<void> {
    if (this.disposed) return;
    this.connectionRequested = true;
    if (this.connecting) return;
    this.connecting = true;
    try {
      await this.openStream();
    } finally {
      this.connecting = false;
    }
  }

  private async openStream(): Promise<void> {
    const sentWith = stampCredentials();
    let token: string | null;
    try {
      token = await this.getToken();
    } catch (error) {
      if (this.disposed || !this.connectionRequested) return;
      this.onError(error);
      this.scheduleReconnect();
      return;
    }
    if (this.disposed || !this.connectionRequested) return;
    if (!token) {
      this.scheduleReconnect();
      return;
    }

    this.closeConnection();

    this.processedLength = 0;
    this.buffer = '';
    this.lastEventIdAtOpen = this.lastEventId;

    const xhr = new XMLHttpRequest();
    this.xhr = xhr;

    xhr.open('GET', this.url);
    xhr.setRequestHeader('Authorization', `Bearer ${token}`);
    xhr.setRequestHeader('Accept', 'text/event-stream');
    const correlationId = newCorrelationId();
    if (correlationId !== null) {
      xhr.setRequestHeader(CORRELATION_HEADER, correlationId);
    }
    if (this.lastEventId) {
      xhr.setRequestHeader('Last-Event-ID', this.lastEventId);
    }

    xhr.onprogress = () => {
      if (this.disposed || isRefusal(xhr.status)) return;
      const newText = xhr.responseText.substring(this.processedLength);
      this.processedLength = xhr.responseText.length;
      if (newText.length > 0) {
        this.reconnectAttempt = 0;
        this.episodeReported = false;
        this.armWatchdog();
      }
      this.applyChunk(newText);
      if (xhr.responseText.length >= MAX_RESPONSE_BYTES) {
        this.reconnectAtCap();
      }
    };

    xhr.onerror = () => {
      if (!this.disposed) {
        this.onError(new SSEConnectionError(correlationId));
        this.scheduleReconnect();
      }
    };

    xhr.onloadend = () => {
      if (this.disposed) return;
      if (!isRefusal(xhr.status)) {
        this.scheduleReconnect();
        return;
      }
      if (xhr.status === HTTP_UNAUTHORIZED) markSessionExpired(sentWith);
      this.onError(new SSEHttpError(xhr.status, correlationId));
      this.scheduleReconnect(retryAfterMs(xhr.getResponseHeader('Retry-After'), Date.now()));
    };

    this.armWatchdog();
    xhr.send();
  }

  disconnect(): void {
    this.connectionRequested = false;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.closeConnection();
  }

  private closeConnection(): void {
    this.clearWatchdog();
    const xhr = this.xhr;
    if (!xhr) return;
    xhr.onprogress = null;
    xhr.onerror = null;
    xhr.onloadend = null;
    xhr.abort();
    this.xhr = null;
  }

  dispose(): void {
    this.disposed = true;
    this.disconnect();
  }

  private forceReconnect(): void {
    if (this.disposed) return;
    this.closeConnection();
    void this.connect();
  }

  private reconnectAtCap(): void {
    if (this.disposed) return;
    if (this.lastEventId !== this.lastEventIdAtOpen) {
      this.stalledCaps = 0;
      this.forceReconnect();
      return;
    }
    this.backOffAtCap();
  }

  private backOffAtCap(): void {
    this.closeConnection();
    this.scheduleReconnect(reconnectDelayMs(this.stalledCaps, 0));
    this.stalledCaps += 1;
  }

  private armWatchdog(): void {
    this.clearWatchdog();
    this.watchdogTimer = setTimeout(() => {
      this.watchdogTimer = null;
      this.forceReconnect();
    }, HEARTBEAT_WATCHDOG_MS);
  }

  private clearWatchdog(): void {
    if (this.watchdogTimer) {
      clearTimeout(this.watchdogTimer);
      this.watchdogTimer = null;
    }
  }

  private scheduleReconnect(minimumDelayMs = 0): void {
    if (this.disposed || this.reconnectTimer) return;
    this.clearWatchdog();
    if (!this.episodeReported) {
      this.episodeReported = true;
      recordEvent({ type: 'sse_reconnect', payload: {} }).catch(() => undefined);
    }
    const delay = reconnectDelayMs(this.reconnectAttempt, minimumDelayMs);
    this.reconnectAttempt += 1;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      void this.connect();
    }, delay);
  }

  private applyChunk(text: string): void {
    const parsed = parseChunk(this.buffer, text, this.lastEventId);
    this.buffer = parsed.remainder;
    this.lastEventId = parsed.lastEventId;

    for (const block of parsed.blocks) {
      if (isServerEvent(block)) this.dispatchEvent(block);
      else this.onError(block);
    }
  }

  private dispatchEvent(event: ServerEvent): void {
    try {
      this.onEvent(event);
    } catch (error) {
      this.onError(new ServerEventHandlerError(event.id, event.type, error));
    }
  }
}
