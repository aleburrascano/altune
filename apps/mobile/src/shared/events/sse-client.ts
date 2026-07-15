/**
 * Lightweight SSE client for React Native.
 *
 * React Native has no native EventSource. This uses XMLHttpRequest's
 * progressive response to parse SSE events, which works reliably
 * across Hermes and JSC.
 *
 * Reliability (see the realtime audit, Wave 1):
 * - Heartbeat watchdog (F2): the server pings every ~25s; if no bytes arrive for
 *   HEARTBEAT_WATCHDOG_MS the socket is treated as silently dead and force-
 *   reconnected (XHR onprogress just stops on a proxy idle-drop — no onerror).
 * - Response recycling (F3): xhr.responseText retains the whole stream for the
 *   connection's life. Past MAX_RESPONSE_BYTES the connection is recycled
 *   (reconnect with Last-Event-ID) so memory stays bounded on hours-long streams.
 * - Backoff with jitter (F4): reconnects use exponential backoff + jitter to
 *   avoid a thundering herd on outage recovery.
 */

// How long a stream may be silent before we assume the socket is dead. The
// server heartbeats every ~25s, so 60s of silence means a dropped connection.
export const HEARTBEAT_WATCHDOG_MS = 60_000;
// Recycle the XHR once its retained responseText grows past this, to bound
// memory on a long-lived stream.
export const MAX_RESPONSE_BYTES = 512 * 1024;

const BASE_RECONNECT_MS = 1_000;
const MAX_RECONNECT_MS = 30_000;

export interface ServerEvent {
  id: string;
  type: string;
  data: Record<string, unknown>;
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
  private watchdogTimer: ReturnType<typeof setTimeout> | null = null;
  private disposed = false;

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

    const token = await this.getToken();
    if (!token || this.disposed) return;

    // Close any existing connection first. connect() can be invoked again while
    // one is already open (AppState 'active', a scheduled reconnect, or a token
    // race); without this the previous XHR keeps streaming and the server
    // accrues duplicate SSE connections (the connection-churn bug).
    this.closeConnection();

    this.processedLength = 0;
    this.buffer = '';

    const xhr = new XMLHttpRequest();
    this.xhr = xhr;

    xhr.open('GET', this.url);
    xhr.setRequestHeader('Authorization', `Bearer ${token}`);
    xhr.setRequestHeader('Accept', 'text/event-stream');
    if (this.lastEventId) {
      xhr.setRequestHeader('Last-Event-ID', this.lastEventId);
    }

    xhr.onprogress = () => {
      if (this.disposed) return;
      const newText = xhr.responseText.substring(this.processedLength);
      this.processedLength = xhr.responseText.length;
      if (newText.length > 0) {
        // Bytes arrived (real event or heartbeat): the connection is live —
        // reset backoff and rearm the watchdog.
        this.reconnectAttempt = 0;
        this.armWatchdog();
      }
      this.parseChunk(newText);
      if (xhr.responseText.length >= MAX_RESPONSE_BYTES) {
        this.forceReconnect(); // recycle to bound memory; Last-Event-ID preserved
      }
    };

    xhr.onerror = () => {
      if (!this.disposed) {
        this.onError(new Error('SSE connection error'));
        this.scheduleReconnect();
      }
    };

    xhr.onloadend = () => {
      if (!this.disposed) {
        this.scheduleReconnect();
      }
    };

    this.armWatchdog();
    xhr.send();
  }

  disconnect(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.closeConnection();
  }

  /**
   * Aborts the active XHR after detaching its handlers, so the teardown does not
   * fire onerror/onloadend -> scheduleReconnect (which would turn an intentional
   * close into a reconnect storm). Also disarms the watchdog for the dead socket.
   */
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

  /**
   * Proactive recovery: the current socket is either silently dead (watchdog) or
   * being recycled (F3). Tear it down without triggering the onloadend backoff
   * path and reconnect immediately, preserving Last-Event-ID.
   */
  private forceReconnect(): void {
    if (this.disposed) return;
    this.closeConnection();
    void this.connect();
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

  private scheduleReconnect(): void {
    if (this.disposed || this.reconnectTimer) return;
    this.clearWatchdog();
    const base = Math.min(BASE_RECONNECT_MS * 2 ** this.reconnectAttempt, MAX_RECONNECT_MS);
    const delay = base + Math.random() * base; // full jitter up to 1x base
    this.reconnectAttempt += 1;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      void this.connect();
    }, delay);
  }

  private parseChunk(text: string): void {
    this.buffer += text;
    const blocks = this.buffer.split('\n\n');
    this.buffer = blocks.pop() ?? '';

    for (const block of blocks) {
      if (!block.trim()) continue;
      const event = this.parseBlock(block);
      if (event) {
        // Only advance the cursor for real, id-bearing events. Control events
        // (e.g. `event: resync`) carry no id — blanking lastEventId would drop
        // the replay cursor on the next reconnect.
        if (event.id) this.lastEventId = event.id;
        this.onEvent(event);
      }
    }
  }

  private parseBlock(block: string): ServerEvent | null {
    let id = '';
    let type = 'message';
    let dataLine = '';

    for (const line of block.split('\n')) {
      if (line.startsWith('id: ')) {
        id = line.substring(4);
      } else if (line.startsWith('event: ')) {
        type = line.substring(7);
      } else if (line.startsWith('data: ')) {
        dataLine = line.substring(6);
      }
    }

    if (!dataLine) return null;

    try {
      const data = JSON.parse(dataLine) as Record<string, unknown>;
      return { id, type, data };
    } catch {
      return null;
    }
  }
}
