const listeners = [];
let log = [];
let held = [];
let reloadCount = 0;

function send(command) {
  const latest = listeners[listeners.length - 1];
  if (latest !== undefined) latest({ command });
}

function addWidgetCommandListener(listener) {
  listeners.push(listener);
  log.push('subscribe');
  return {
    remove() {
      const at = listeners.indexOf(listener);
      if (at >= 0) listeners.splice(at, 1);
      log.push('remove');
    },
  };
}

function drainPendingCommands() {
  log.push('drain');
  for (const command of held.splice(0)) send(command);
}

function reloadWidget() {
  reloadCount += 1;
}

const __widget = {
  reset() {
    listeners.length = 0;
    log = [];
    held = [];
    reloadCount = 0;
  },
  send,
  hold(...commands) {
    held.push(...commands);
  },
  reloads: () => reloadCount,
  log: () => [...log],
};

module.exports = {
  addWidgetCommandListener,
  drainPendingCommands,
  reloadWidget,
  writeSnapshot() {},
  writeCoverFromUrl() {
    return Promise.resolve();
  },
  __widget,
};
