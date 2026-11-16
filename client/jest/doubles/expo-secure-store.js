const items = new Map();
const pendingFailures = new Map();
const optionsSeen = { get: [], set: [], delete: [] };

const AFTER_FIRST_UNLOCK = 'AFTER_FIRST_UNLOCK';
const WHEN_UNLOCKED = 'WHEN_UNLOCKED';

function takeFailure(operation) {
  const failure = pendingFailures.get(operation);
  if (failure === undefined) return;
  pendingFailures.delete(operation);
  throw failure;
}

async function getItemAsync(key, options) {
  optionsSeen.get.push(options);
  takeFailure('get');
  return items.has(key) ? items.get(key) : null;
}

async function setItemAsync(key, value, options) {
  optionsSeen.set.push(options);
  takeFailure('set');
  items.set(key, String(value));
}

async function deleteItemAsync(key, options) {
  optionsSeen.delete.push(options);
  takeFailure('delete');
  items.delete(key);
}

async function isAvailableAsync() {
  return !pendingFailures.has('unavailable');
}

const __secureStore = {
  reset() {
    items.clear();
    pendingFailures.clear();
    for (const seen of Object.values(optionsSeen)) seen.length = 0;
  },

  optionsOf(operation) {
    return [...optionsSeen[operation]];
  },

  seed(key, value) {
    items.set(key, String(value));
  },

  read(key) {
    return items.get(key);
  },

  keys() {
    return [...items.keys()];
  },

  failNext(operation, error) {
    pendingFailures.set(operation, error ?? new Error(`injected keychain ${operation} failure`));
  },
};

module.exports = {
  AFTER_FIRST_UNLOCK,
  WHEN_UNLOCKED,
  getItemAsync,
  setItemAsync,
  deleteItemAsync,
  isAvailableAsync,
  __secureStore,
};
