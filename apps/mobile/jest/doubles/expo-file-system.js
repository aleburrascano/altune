const fileContents = new Map();
const directories = new Set();

const pendingFailures = {
  write: null,
  read: null,
  delete: null,
  download: null,
  createDirectory: null,
  list: null,
};

const Paths = {
  document: 'file:///document',
  cache: 'file:///cache',
};

function uriOf(base) {
  return typeof base === 'string' ? base : base.uri;
}

function resolve(base, name) {
  return name === undefined ? uriOf(base) : `${uriOf(base)}/${name}`;
}

function takeFailure(kind) {
  const failure = pendingFailures[kind];
  if (failure === null) return;
  pendingFailures[kind] = null;
  throw failure;
}

function childUris(parentUri) {
  const prefix = `${parentUri}/`;
  const direct = new Set();
  for (const uri of [...fileContents.keys(), ...directories]) {
    if (!uri.startsWith(prefix)) continue;
    const rest = uri.slice(prefix.length);
    const cut = rest.indexOf('/');
    direct.add(cut === -1 ? uri : `${prefix}${rest.slice(0, cut)}`);
  }
  return [...direct];
}

class Directory {
  constructor(base, name) {
    this.uri = resolve(base, name);
  }

  get exists() {
    return directories.has(this.uri);
  }

  create(options) {
    takeFailure('createDirectory');
    if (directories.has(this.uri) && options?.idempotent !== true) {
      throw new Error(`ERR_DIRECTORY_EXISTS: directory already exists, ${this.uri}`);
    }
    directories.add(this.uri);
  }

  delete() {
    takeFailure('delete');
    directories.delete(this.uri);
    for (const uri of [...fileContents.keys()]) {
      if (uri.startsWith(`${this.uri}/`)) fileContents.delete(uri);
    }
  }

  list() {
    takeFailure('list');
    if (!this.exists) throw new Error(`ENOENT: no such directory, ${this.uri}`);
    return childUris(this.uri).map((uri) =>
      fileContents.has(uri) ? fileFromUri(uri) : directoryFromUri(uri),
    );
  }
}

class File {
  constructor(base, name) {
    this.uri = resolve(base, name);
  }

  get exists() {
    return fileContents.has(this.uri);
  }

  get size() {
    const contents = fileContents.get(this.uri);
    return contents === undefined ? null : contents.length;
  }

  textSync() {
    takeFailure('read');
    const contents = fileContents.get(this.uri);
    if (contents === undefined) throw new Error(`ENOENT: no such file, ${this.uri}`);
    return contents;
  }

  write(contents) {
    takeFailure('write');
    fileContents.set(this.uri, String(contents));
  }

  delete() {
    takeFailure('delete');
    fileContents.delete(this.uri);
  }

  static async downloadFileAsync(url, dest) {
    takeFailure('download');
    fileContents.set(dest.uri, `downloaded:${url}`);
    return fileFromUri(dest.uri);
  }
}

function fileFromUri(uri) {
  return new File(uri);
}

function directoryFromUri(uri) {
  return new Directory(uri);
}

const __fs = {
  reset() {
    fileContents.clear();
    directories.clear();
    for (const kind of Object.keys(pendingFailures)) pendingFailures[kind] = null;
  },

  seedDirectory(uri) {
    directories.add(uri);
  },

  seedFile(uri, contents) {
    const cut = uri.lastIndexOf('/');
    if (cut !== -1) directories.add(uri.slice(0, cut));
    fileContents.set(uri, String(contents));
  },

  readFile(uri) {
    return fileContents.get(uri);
  },

  allFiles() {
    return Object.fromEntries(fileContents);
  },

  failNext(kind, error) {
    if (!(kind in pendingFailures)) {
      throw new Error(`unknown failure kind "${kind}" — expected one of ${Object.keys(pendingFailures).join(', ')}`);
    }
    pendingFailures[kind] = error ?? new Error(`injected ${kind} failure`);
  },
};

module.exports = { Directory, File, Paths, __fs };
