/**
 * Lightweight SSE client for React Native.
 *
 * React Native has no native EventSource. This uses XMLHttpRequest's
 * progressive response to parse SSE events, which works reliably
 * across Hermes and JSC.
 */

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
      this.parseChunk(newText);
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

    xhr.send();
  }

  disconnect(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.xhr) {
      this.xhr.abort();
      this.xhr = null;
    }
  }

  dispose(): void {
    this.disposed = true;
    this.disconnect();
  }

  private scheduleReconnect(): void {
    if (this.disposed || this.reconnectTimer) return;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      void this.connect();
    }, 3000);
  }

  private parseChunk(text: string): void {
    this.buffer += text;
    const blocks = this.buffer.split('\n\n');
    this.buffer = blocks.pop() ?? '';

    for (const block of blocks) {
      if (!block.trim()) continue;
      const event = this.parseBlock(block);
      if (event) {
        this.lastEventId = event.id;
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
