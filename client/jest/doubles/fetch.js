class FakeResponse {
  constructor(rule) {
    this.status = rule.status;
    this.ok = rule.status >= 200 && rule.status < 300;
    this._json = rule.json;
    this._malformed = rule.malformed === true;
    const lowered = Object.entries(rule.headers ?? {}).map(([k, v]) => [
      k.toLowerCase(),
      String(v),
    ]);
    const byName = new Map(lowered);
    this.headers = { get: (name) => byName.get(String(name).toLowerCase()) ?? null };
  }

  async json() {
    if (this._malformed) throw new SyntaxError('Unexpected end of JSON input');
    return this._json;
  }
}

function abortError() {
  const error = new Error('The operation was aborted');
  error.name = 'AbortError';
  return error;
}

function transportError() {
  return new TypeError('Network request failed');
}

function pathnameOf(url) {
  const withoutQuery = String(url).split('?')[0];
  const scheme = withoutQuery.indexOf('://');
  if (scheme === -1) return withoutQuery;
  const slash = withoutQuery.indexOf('/', scheme + 3);
  return slash === -1 ? '/' : withoutQuery.slice(slash);
}

function parseSpec(spec) {
  const parts = String(spec).trim().split(/\s+/);
  if (parts.length === 2) return { method: parts[0].toUpperCase(), path: parts[1] };
  return { method: null, path: parts[0] };
}

function normalize(response) {
  const rule = response ?? {};
  return {
    status: rule.status ?? 200,
    json: rule.json,
    malformed: rule.malformed === true,
    headers: rule.headers,
  };
}

const requests = [];
const rules = [];
const hanging = new Set();
let fallback = null;

function matches(rule, method, path) {
  if (rule.method !== null && rule.method !== method) return false;
  return rule.path === path;
}

function takeRule(method, path) {
  const index = rules.findIndex((rule) => matches(rule, method, path));
  if (index === -1) return null;
  const rule = rules[index];
  if (rule.once) rules.splice(index, 1);
  return rule;
}

function headersOf(init) {
  const headers = init.headers ?? {};
  return typeof headers.forEach === 'function' && typeof headers.get === 'function'
    ? Object.fromEntries(headers.entries())
    : { ...headers };
}

async function fakeFetch(url, init = {}) {
  const method = String(init.method ?? 'GET').toUpperCase();
  const path = pathnameOf(url);
  const signal = init.signal;
  requests.push({
    url: String(url),
    path,
    query: String(url).includes('?') ? String(url).slice(String(url).indexOf('?') + 1) : '',
    method,
    headers: headersOf(init),
    body: init.body,
    signal,
  });

  const rule = takeRule(method, path) ?? fallback;
  if (rule === null) {
    throw new Error(`http double: no rule registered for ${method} ${path}`);
  }

  if (signal?.aborted === true) throw abortError();

  if (rule.hang === true) {
    return new Promise((_resolve, reject) => {
      const giveUp = () => reject(abortError());
      hanging.add(giveUp);
      if (signal == null) return;
      signal.addEventListener('abort', giveUp, { once: true });
    });
  }

  if (rule.reject !== undefined) throw rule.reject;

  if (rule.gate !== undefined) await rule.gate;

  return new FakeResponse(rule);
}

const __http = {
  abortHanging() {
    for (const giveUp of hanging) giveUp();
    hanging.clear();
  },

  reset() {
    requests.length = 0;
    rules.length = 0;
    fallback = null;
  },

  reply(spec, response) {
    rules.push({ ...parseSpec(spec), ...normalize(response), once: false });
    return __http;
  },

  replyOnce(spec, response) {
    rules.push({ ...parseSpec(spec), ...normalize(response), once: true });
    return __http;
  },

  replyAll(response) {
    fallback = { ...normalize(response), hang: false };
    return __http;
  },

  hang(spec) {
    rules.push({ ...parseSpec(spec), hang: true, once: false });
    return __http;
  },

  hold(spec, response) {
    let release = () => {};
    const gate = new Promise((resolve) => {
      release = resolve;
    });
    rules.push({ ...parseSpec(spec), ...normalize(response), gate, once: false });
    return release;
  },

  fail(spec, error) {
    rules.push({ ...parseSpec(spec), reject: error ?? transportError(), once: false });
    return __http;
  },

  requests,

  last() {
    return requests[requests.length - 1];
  },

  countFor(spec) {
    const wanted = parseSpec(spec);
    return requests.filter((request) => matches({ ...wanted }, request.method, request.path))
      .length;
  },

  abortError,
  transportError,
};

module.exports = { fakeFetch, __http, FakeResponse };
