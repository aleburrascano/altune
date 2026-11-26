type CommandListener = (event: { command: string }) => void;

type Operation = 'subscribe' | 'drain' | 'remove';

type CoverWriter = (url: string) => Promise<void>;

const NATIVE_MODULE_NAME = 'AltuneWidget';

export type NativeWidget = {
  reset(): void;
  forgetWrites(): void;
  send(command: string): void;
  hold(...commands: string[]): void;
  coverWriter(writer: CoverWriter): void;
  coverWriterOnce(writer: CoverWriter): void;
  reloads(): number;
  operations(): Operation[];
  snapshots(): unknown[];
  coverWrites(): [url: string, fileName: string][];
};

const writeCoverAtOnce: CoverWriter = () => Promise.resolve();

function installNativeWidget(): NativeWidget {
  const listeners: CommandListener[] = [];
  let operations: Operation[] = [];
  let held: string[] = [];
  let reloadCount = 0;
  let snapshots: unknown[] = [];
  let coverWrites: [string, string][] = [];
  let writeCover: CoverWriter = writeCoverAtOnce;
  let queuedCoverWriters: CoverWriter[] = [];

  function send(command: string): void {
    listeners[listeners.length - 1]?.({ command });
  }

  function forgetWrites(): void {
    reloadCount = 0;
    snapshots = [];
    coverWrites = [];
  }

  globalThis.expo.modules[NATIVE_MODULE_NAME] = {
    addListener(_eventName: string, listener: CommandListener) {
      listeners.push(listener);
      operations.push('subscribe');
      return {
        remove() {
          const at = listeners.indexOf(listener);
          if (at >= 0) listeners.splice(at, 1);
          operations.push('remove');
        },
      };
    },
    drainPendingCommands() {
      operations.push('drain');
      for (const command of held.splice(0)) send(command);
    },
    reloadWidget() {
      reloadCount += 1;
    },
    writeSnapshot(json: string) {
      snapshots.push(JSON.parse(json));
    },
    writeCoverFromUrl(url: string, fileName: string) {
      coverWrites.push([url, fileName]);
      return (queuedCoverWriters.shift() ?? writeCover)(url);
    },
  };

  return {
    reset() {
      listeners.length = 0;
      operations = [];
      held = [];
      writeCover = writeCoverAtOnce;
      queuedCoverWriters = [];
      forgetWrites();
    },
    forgetWrites,
    send,
    hold(...commands) {
      held.push(...commands);
    },
    coverWriter(writer) {
      writeCover = writer;
    },
    coverWriterOnce(writer) {
      queuedCoverWriters.push(writer);
    },
    reloads: () => reloadCount,
    operations: () => [...operations],
    snapshots: () => [...snapshots],
    coverWrites: () => [...coverWrites],
  };
}

export const nativeWidget = installNativeWidget();
