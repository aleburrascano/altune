import {
  deleteDocument,
  readDocument,
  readVersionedEntries,
  writeDocumentAtomically,
  type SchemaSpec,
} from '@shared/files/durableDocument';
import type { StoredDirectory } from '@shared/files/fileStore';
import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

const DIR_URI = 'memory://document/docs';
const FILE_URI = `${DIR_URI}/doc.json`;
const TEMP_URI = `${DIR_URI}/doc.json.tmp`;

let store: MemoryFileStore;
let dir: StoredDirectory;
let warn: jest.SpyInstance;

beforeEach(() => {
  store = createMemoryFileStore();
  dir = store.openDirectory('docs');
  dir.create();
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warn.mockRestore();
});

function read() {
  return readDocument('[test]', 'doc.json', () => dir);
}

describe('writeDocumentAtomically', () => {
  it('commits the contents under the name and leaves no temp file behind', () => {
    writeDocumentAtomically(dir, 'doc.json', '{"a":1}');
    writeDocumentAtomically(dir, 'doc.json', '{"a":2}');

    expect(store.files.get(FILE_URI)).toBe('{"a":2}');
    expect(store.files.has(TEMP_URI)).toBe(false);
  });

  it('a write that dies before the rename leaves the committed file intact', () => {
    store.files.set(FILE_URI, '{"a":1}');
    const failingDir: StoredDirectory = {
      ...dir,
      openFile: (name) => {
        const file = dir.openFile(name);
        return {
          ...file,
          moveTo: () => {
            throw new Error('killed');
          },
        };
      },
    };

    expect(() => writeDocumentAtomically(failingDir, 'doc.json', '{"a":')).toThrow('killed');

    expect(read()).toEqual({ status: 'read', document: { a: 1 } });
  });
});

describe('deleteDocument', () => {
  it('removes the committed file and a leftover temp file', () => {
    store.files.set(FILE_URI, '{}');
    store.files.set(TEMP_URI, '{}');

    deleteDocument(dir, 'doc.json');

    expect(store.files.size).toBe(0);
  });

  it('is a no-op when neither exists', () => {
    expect(() => deleteDocument(dir, 'doc.json')).not.toThrow();
  });
});

describe('readDocument', () => {
  it('reports a missing document as absent, without a warning', () => {
    expect(read()).toEqual({ status: 'absent' });
    expect(warn).not.toHaveBeenCalled();
  });

  it('prefers the committed file over a half-written temp file', () => {
    store.files.set(FILE_URI, '{"a":1}');
    store.files.set(TEMP_URI, '{"a":');

    expect(read()).toEqual({ status: 'read', document: { a: 1 } });
  });

  it('falls back to the temp file when a kill landed between removing the old file and the rename', () => {
    store.files.set(TEMP_URI, '{"a":2}');

    expect(read()).toEqual({ status: 'read', document: { a: 2 } });
  });

  it('recovers a complete temp write when the committed file is corrupt, still warning about the corruption', () => {
    store.files.set(FILE_URI, '{"a":');
    store.files.set(TEMP_URI, '{"a":3}');

    expect(read()).toEqual({ status: 'read', document: { a: 3 } });
    expect(warn).toHaveBeenCalledWith(
      '[test] doc.json is corrupt (SyntaxError); trying doc.json.tmp',
    );
  });

  it('warns for each damaged candidate when neither the committed nor the temp file parses', () => {
    store.files.set(FILE_URI, '{"a":');
    store.files.set(TEMP_URI, '{"b":');

    expect(read()).toEqual({ status: 'unreadable' });
    expect(warn.mock.calls).toEqual([
      ['[test] doc.json is corrupt (SyntaxError); trying doc.json.tmp'],
      ['[test] doc.json.tmp is corrupt (SyntaxError); treating it as empty'],
    ]);
  });

  it('warns that a file which exists but cannot be read could not be read', () => {
    const unreadableDir: StoredDirectory = {
      ...dir,
      openFile: (name) => ({
        ...dir.openFile(name),
        exists: true,
        textSync: () => {
          throw new RangeError('EACCES');
        },
      }),
    };

    expect(readDocument('[test]', 'doc.json', () => unreadableDir)).toEqual({
      status: 'unreadable',
    });
    expect(warn).toHaveBeenLastCalledWith(
      '[test] doc.json.tmp could not be read (RangeError); treating it as empty',
    );
  });

  it('warns naming the file and the parse error, never the contents, for a corrupt file', () => {
    store.files.set(FILE_URI, '{"secret-user-data": "tr');

    expect(read()).toEqual({ status: 'unreadable' });
    expect(warn).toHaveBeenCalledTimes(1);
    expect(warn).toHaveBeenCalledWith(
      '[test] doc.json is corrupt (SyntaxError); treating it as empty',
    );
  });

  it('warns naming the file and the error for a file that cannot be read', () => {
    const failing = (): StoredDirectory => {
      throw new TypeError('EIO');
    };

    expect(readDocument('[test]', 'doc.json', failing)).toEqual({ status: 'unreadable' });
    expect(warn).toHaveBeenCalledWith(
      '[test] could not read doc.json (TypeError); treating it as empty',
    );
  });

  it('names a non-Error throw by its type', () => {
    const failing = (): StoredDirectory => {
      throw 'EIO';
    };

    readDocument('[test]', 'doc.json', failing);

    expect(warn).toHaveBeenCalledWith(
      '[test] could not read doc.json (string); treating it as empty',
    );
  });
});

describe('reading a versioned document', () => {
  const spec: SchemaSpec = {
    current: 2,
    migrations: [
      (bare) => ({ schemaVersion: 1, entries: { items: bare } }),
      (v1) => {
        const doc = v1 as { entries: object };
        return { ...doc, schemaVersion: 2, entries: { ...doc.entries, extra: true } };
      },
    ],
  };

  function readStored(document: unknown, schema: SchemaSpec = spec) {
    store.files.set(FILE_URI, JSON.stringify(document));
    return readVersionedEntries('[test]', 'doc.json', () => dir, schema);
  }

  it.each<[string, unknown]>([
    ['a bare array', ['a', 'b']],
    ['null', null],
    ['a number', 'not-a-document'.length],
    ['an object with no version', { t1: {} }],
    ['a non-integer version', { schemaVersion: 1.5 }],
    ['a negative version', { schemaVersion: -1 }],
    ['a string version', { schemaVersion: '1' }],
  ])('treats %s as unversioned and migrates it from version 0', (_label, document) => {
    expect(readStored(document)).toEqual({
      status: 'read',
      entries: { items: document, extra: true },
    });
  });

  it('reads the version a document is stamped with', () => {
    readStored({ schemaVersion: 3, entries: [] });

    expect(warn).toHaveBeenCalledWith(expect.stringContaining('has schema version 3'));
  });

  it('runs only the remaining migrations from an intermediate version', () => {
    expect(readStored({ schemaVersion: 1, entries: { items: ['a'] } })).toEqual({
      status: 'read',
      entries: { items: ['a'], extra: true },
    });
  });

  it('returns a current document untouched', () => {
    expect(readStored({ schemaVersion: 2, entries: { items: [] } })).toEqual({
      status: 'read',
      entries: { items: [] },
    });
  });

  it('throws when a spec is missing a migration step', () => {
    expect(() => readStored([], { current: 1, migrations: [] })).toThrow(
      'no migration from schema version 0',
    );
  });
});

describe('readVersionedEntries', () => {
  const spec: SchemaSpec = {
    current: 1,
    migrations: [(bare) => ({ schemaVersion: 1, entries: bare })],
  };

  function readEntries() {
    return readVersionedEntries('[test]', 'doc.json', () => dir, spec);
  }

  it('passes an absent document through', () => {
    expect(readEntries()).toEqual({ status: 'absent' });
  });

  it('passes an unreadable document through', () => {
    store.files.set(FILE_URI, '{');

    expect(readEntries()).toEqual({ status: 'unreadable' });
  });

  it('returns the entries of a legacy document after migrating it', () => {
    store.files.set(FILE_URI, '[1,2]');

    expect(readEntries()).toEqual({ status: 'read', entries: [1, 2] });
    expect(warn).not.toHaveBeenCalled();
  });

  it('returns the entries of a newer document best-effort, with a warning', () => {
    store.files.set(FILE_URI, '{"schemaVersion":4,"entries":[1]}');

    expect(readEntries()).toEqual({ status: 'read', entries: [1] });
    expect(warn).toHaveBeenCalledWith(
      '[test] doc.json has schema version 4, newer than 1; reading what this version understands',
    );
  });
});
