type CommandListener = (event: { command: string }) => void;

type Operation = 'subscribe' | 'drain' | 'remove';

const NATIVE_MODULE_NAME = 'AltuneWidget';

export type NativeWidget = {
  reset(): void;
  send(command: string): void;
  hold(...commands: string[]): void;
  reloads(): number;
  operations(): Operation[];
};

function installNativeWidget(): NativeWidget {
  const listeners: CommandListener[] = [];
  let operations: Operation[] = [];
  let held: string[] = [];
  let reloadCount = 0;

  function send(command: string): void {
    listeners[listeners.length - 1]?.({ command });
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
    writeSnapshot() {},
    writeCoverFromUrl() {
      return Promise.resolve();
    },
  };

  return {
    reset() {
      listeners.length = 0;
      operations = [];
      held = [];
      reloadCount = 0;
    },
    send,
    hold(...commands) {
      held.push(...commands);
    },
    reloads: () => reloadCount,
    operations: () => [...operations],
  };
}

export const nativeWidget = installNativeWidget();
