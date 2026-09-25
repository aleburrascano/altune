import { CORRELATION_HEADER, newCorrelationId } from '@shared/api-client/correlationId';
import { markSessionExpired, stampCredentials } from '@shared/auth/sessionExpired';

export const HEARTBEAT_WATCHDOG_MS = 60_000;
export const MAX_RESPONSE_BYTES = 512 * 1024;

const BASE_RECONNECT_MS = 1_000;
const MAX_RECONNECT_MS = 30_000;
const MAX_RETRY_AFTER_MS = 5 * 60_000;
const UNAUTHORIZED = 401;

export interface ServerEvent {
  id: string;
  type: string;
  data: Record<string, unknown>;
}

function reconnectDelayMs(attempt: number, minimumDelayMs: number): number {
  const base = Math.min(BASE_RECONNECT_MS * 2 ** attempt, MAX_RECONNECT_MS);
  return Math.max(base + Math.random() * base, minimumDelayMs);
}

/**
 * A block whose `data:` payload is not valid JSON. Carries only the event's id, type and payload
 * length: the payload itself (and the parser's SyntaxError, which quotes it) may hold user data.
 */
export class MalformedSSEEventError extends Error {
  readonly eventId: string;
  readonly eventType: string;
  readonly payloadLength: number;

  constructor(eventId: string, eventType: string, payloadLength: number) {
    super(
      `malformed SSE event payload (type=${eventType}, id=${eventId || '<none>'}, length=${payloadLength})`,
    );
    this.name = 'MalformedSSEEventError';
    this.eventId = eventId;
    this.eventType = eventType;
    this.payloadLength = payloadLength;
  }
}

/**
 * A handler threw while applying an event. Carries the thrown error as `cause`: it comes from the
 * app's own dispatch table, not from a parser quoting the wire payload.
 */
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

/** The stream failed at the transport; carries the id the request was sent with. */
export class SSEConnectionError extends Error {
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

type WireField = { name: string; value: string };

function fieldOf(line: string): WireField | null {
  if (line.startsWith(':')) return null;
  const colon = line.indexOf(':');
  if (colon === -1) return null;
  const name = line.substring(0, colon);
  const raw = line.substring(colon + 1);
  return { name, value: raw.startsWith(' ') ? raw.substring(1) : raw };
}

/** A block either carries an event or explains why it could not; both keep their place in the chunk. */
type ParsedBlock = ServerEvent | MalformedSSEEventError;

interface ParsedChunk {
  blocks: ParsedBlock[];
  remainder: string;
  lastEventId: string;
}

function isServerEvent(block: ParsedBlock): block is ServerEvent {
  return !(block instanceof MalformedSSEEventError);
}

function withUnixLineEndings(text: string): string {
  return text.replace(/\r\n/g, '\n').replace(/\r/g, '\n');
}

/** Null when the block carries no `data:` line: heartbeat padding, a comment, or a bare `retry:`. */
function parseBlock(block: string): ParsedBlock | null {
  let id = '';
  let type = 'message';
  const dataLines: string[] = [];

  for (const line of block.split('\n')) {
    const field = fieldOf(line);
    if (field === null) continue;
    if (field.name === 'id') id = field.value;
    else if (field.name === 'event') type = field.value;
    else if (field.name === 'data') dataLines.push(field.value);
  }

  if (dataLines.length === 0) return null;

  const payload = dataLines.join('\n');
  try {
    return { id, type, data: JSON.parse(payload) as Record<string, unknown> };
  } catch {
    return new MalformedSSEEventError(id, type, payload.length);
  }
}

function latestEventId(blocks: ParsedBlock[], fallback: string): string {
  let latest = fallback;
  for (const block of blocks) {
    if (isServerEvent(block) && block.id) latest = block.id;
  }
  return latest;
}

/**
 * Splits `buffer + chunk` on the SSE block separator; `remainder` is the trailing partial block the
 * caller must feed back in, since a block can arrive across any number of chunks.
 */
function parseChunk(buffer: string, chunk: string, lastEventId: string): ParsedChunk {
  const rawBlocks = (buffer + withUnixLineEndings(chunk)).split('\n\n');
  const remainder = rawBlocks.pop() ?? '';
  const blocks: ParsedBlock[] = [];
  for (const raw of rawBlocks) {
    const block = parseBlock(raw);
    if (block !== null) blocks.push(block);
  }
  return { blocks, remainder, lastEventId: latestEventId(blocks, lastEventId) };
}

export class SSEClient {
  private xhr: XMLHttpRequest | null = null;
  private lastEventId = '';
  private processedLength = 0;
  private buffer = '';
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnectAttempt = 0;
  private stalledCaps = 0;
  private lastEventIdAtOpen = '';
  private watchdogTimer: ReturnType<typeof setTimeout> | null = null;
  private disposed = false;
  private connecting = false;
  /** Cleared by disconnect(), so a connect() still awaiting its token abandons instead of opening. */
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
      if (xhr.status === UNAUTHORIZED) markSessionExpired(sentWith);
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

  /** Contained per event: one throwing handler must not cost the rest of the chunk's batch. */
  private dispatchEvent(event: ServerEvent): void {
    try {
      this.onEvent(event);
    } catch (error) {
      this.onError(new ServerEventHandlerError(event.id, event.type, error));
    }
  }
}
