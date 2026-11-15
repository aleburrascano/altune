import { CORRELATION_HEADER, newCorrelationId } from '@shared/apiClient';
import { markSessionExpired, stampCredentials, type CredentialStamp } from '@shared/auth';
import { HTTP_UNAUTHORIZED } from '@shared/failure';
import { recordEvent } from '@shared/telemetry';

import { isRefusal, reconnectDelayMs, retryAfterMs } from '@shared/events/core/reconnect';
import { isServerEvent, parseChunk, type ServerEvent } from '@shared/events/core/sseFrames';
import {
  ServerEventHandlerError,
  SSEConnectionError,
  SSEHttpError,
} from '@shared/events/core/streamErrors';

const HEARTBEAT_WATCHDOG_MS = 60_000;
const MAX_RESPONSE_BYTES = 524_288;

type EventHandler = (event: ServerEvent) => void;
type ErrorHandler = (error: unknown) => void;
type RequestListener = ((event: never) => unknown) | null;

export interface StreamRequest {
  readonly responseText: string;
  readonly status: number;
  onprogress: RequestListener;
  onerror: RequestListener;
  onloadend: RequestListener;
  open(method: string, url: string): void;
  setRequestHeader(name: string, value: string): void;
  getResponseHeader(name: string): string | null;
  send(): void;
  abort(): void;
}

export interface StreamIO {
  openRequest(): StreamRequest;
  random(): number;
}

interface OpenedRequest {
  readonly xhr: StreamRequest;
  readonly correlationId: string | null;
  readonly sentWith: CredentialStamp;
}

export class SSEClient {
  private xhr: StreamRequest | null = null;
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
  private io: StreamIO;

  constructor(
    url: string,
    getToken: () => Promise<string | null>,
    onEvent: EventHandler,
    onError: ErrorHandler,
    io: StreamIO,
  ) {
    this.url = url;
    this.getToken = getToken;
    this.onEvent = onEvent;
    this.onError = onError;
    this.io = io;
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

  private abandoned(): boolean {
    return this.disposed || !this.connectionRequested;
  }

  private async openStream(): Promise<void> {
    const sentWith = stampCredentials();
    let token: string | null;
    try {
      token = await this.getToken();
    } catch (error) {
      this.failTokenLookup(error);
      return;
    }
    if (this.abandoned()) return;
    if (!token) {
      this.scheduleReconnect();
      return;
    }
    this.openRequest(token, sentWith);
  }

  private failTokenLookup(error: unknown): void {
    if (this.abandoned()) return;
    this.onError(error);
    this.scheduleReconnect();
  }

  private openRequest(token: string, sentWith: CredentialStamp): void {
    const xhr = this.freshRequest();
    const opened = { xhr, correlationId: this.sendHeaders(xhr, token), sentWith };
    xhr.onprogress = () => this.receive(xhr);
    xhr.onerror = () => this.failConnection(opened.correlationId);
    xhr.onloadend = () => this.endStream(opened);
    this.armWatchdog();
    xhr.send();
  }

  private freshRequest(): StreamRequest {
    this.closeConnection();
    this.processedLength = 0;
    this.buffer = '';
    this.lastEventIdAtOpen = this.lastEventId;
    this.xhr = this.io.openRequest();
    return this.xhr;
  }

  private sendHeaders(xhr: StreamRequest, token: string): string | null {
    xhr.open('GET', this.url);
    xhr.setRequestHeader('Authorization', `Bearer ${token}`);
    xhr.setRequestHeader('Accept', 'text/event-stream');
    const correlationId = newCorrelationId();
    if (correlationId !== null) xhr.setRequestHeader(CORRELATION_HEADER, correlationId);
    if (this.lastEventId) xhr.setRequestHeader('Last-Event-ID', this.lastEventId);
    return correlationId;
  }

  private receive(xhr: StreamRequest): void {
    if (this.disposed || isRefusal(xhr.status)) return;
    const newText = xhr.responseText.substring(this.processedLength);
    this.processedLength = xhr.responseText.length;
    if (newText.length > 0) this.markAlive();
    this.applyChunk(newText);
    if (xhr.responseText.length >= MAX_RESPONSE_BYTES) {
      this.reconnectAtCap();
    }
  }

  private markAlive(): void {
    this.reconnectAttempt = 0;
    this.episodeReported = false;
    this.armWatchdog();
  }

  private failConnection(correlationId: string | null): void {
    if (this.disposed) return;
    this.onError(new SSEConnectionError(correlationId));
    this.scheduleReconnect();
  }

  private endStream(opened: OpenedRequest): void {
    if (this.disposed) return;
    if (isRefusal(opened.xhr.status)) this.refuse(opened);
    else this.scheduleReconnect();
  }

  private refuse({ xhr, correlationId, sentWith }: OpenedRequest): void {
    if (xhr.status === HTTP_UNAUTHORIZED) markSessionExpired(sentWith);
    this.onError(new SSEHttpError(xhr.status, correlationId));
    this.scheduleReconnect(retryAfterMs(xhr.getResponseHeader('Retry-After'), Date.now()));
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
    this.scheduleReconnect(reconnectDelayMs(this.stalledCaps, 0, this.io.random()));
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
    this.reportEpisode();
    const delay = reconnectDelayMs(this.reconnectAttempt, minimumDelayMs, this.io.random());
    this.reconnectAttempt += 1;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      void this.connect();
    }, delay);
  }

  private reportEpisode(): void {
    if (this.episodeReported) return;
    this.episodeReported = true;
    recordEvent({ type: 'sse_reconnect', payload: {} }).catch((error: unknown) => {
      console.warn('[sse] recording reconnect event failed', error);
    });
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
