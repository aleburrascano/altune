import * as FileSystem from 'expo-file-system';

import { NetworkError } from '@shared/api-client';
import { asTrackId } from '@shared/api-client/ids';
import type { SignOutResult } from '@shared/auth/useSignOut';
import { usePinnedStore } from '@shared/offline/pinnedStore';

import { downloadUsage } from '../downloadStatsModel';
import type { RemoveDownloads } from '../hooks/useRemoveDownloads';
import {
  buildDangerZoneActions,
  type ClearHistoryState,
  type DangerZoneActionKey,
} from '../dangerZoneActions';

type Opts = Parameters<typeof buildDangerZoneActions>[0];

function statsFor(count: number, bytes: number, size: string): RemoveDownloads['stats'] {
  const usage = downloadUsage(count, bytes);
  return {
    downloadCount: count,
    downloadBytes: bytes,
    downloadSize: size,
    usage,
    usageLabel: '',
    usageDetail: usage === 'none' ? undefined : size,
  };
}

function makeOpts(
  over: {
    downloads?: {
      count?: number;
      bytes?: number;
      size?: string;
      lastUnpinAll?: RemoveDownloads['lastUnpinAll'];
    };
    signOutState?: SignOutResult;
    clearHistory?: Partial<ClearHistoryState>;
    signOut?: () => Promise<void>;
  } = {},
): Opts {
  const lastUnpinAll = over.downloads?.lastUnpinAll;
  return {
    downloads: {
      stats: statsFor(
        over.downloads?.count ?? 1,
        over.downloads?.bytes ?? 4 * 1024 ** 2,
        over.downloads?.size ?? '4 MB',
      ),
      ...(lastUnpinAll === undefined ? {} : { lastUnpinAll }),
      unpinAll: jest.fn(),
    },
    signOutState: over.signOutState ?? ({ status: 'idle' } as SignOutResult),
    clearHistory: {
      mutate: jest.fn(),
      isPending: false,
      isError: false,
      isSuccess: false,
      error: undefined,
      ...over.clearHistory,
    } satisfies ClearHistoryState,
    signOut: over.signOut ?? jest.fn().mockResolvedValue(undefined),
  };
}

const { __fs } = FileSystem as unknown as {
  __fs: {
    seedFile(uri: string, contents: string): void;
    failNext(kind: 'delete', error?: Error): void;
  };
};

function seedReadyDownload(trackId: string): void {
  const uri = `file:///document/offline-audio/${trackId}.mp3`;
  __fs.seedFile(uri, 'audio-bytes');
  usePinnedStore.setState({
    entries: { [trackId]: { trackId: asTrackId(trackId), status: 'ready', uri } },
    queue: [],
    isWorking: false,
    lastUnpinAll: undefined,
  });
}

type DangerZoneRow = ReturnType<typeof buildDangerZoneActions>[number]['row'];

// Built from the recorded outcome, which is the one the settings screen selects.
function removeDownloadsRow(): DangerZoneRow | undefined {
  const { lastUnpinAll } = usePinnedStore.getState();
  return buildDangerZoneActions(makeOpts({ downloads: { lastUnpinAll } }))[0]?.row;
}

describe('buildDangerZoneActions', () => {
  it('lists downloads, history, sign-out in that order with distinct confirm testIDs', () => {
    const actions = buildDangerZoneActions(makeOpts());
    expect(actions.map((a) => a.key)).toEqual(['downloads', 'history', 'sign-out']);
    expect(new Set(actions.map((a) => a.confirm.testID)).size).toBe(3);
  });

  it('hides only the downloads row when nothing is downloaded', () => {
    const actions = buildDangerZoneActions(
      makeOpts({ downloads: { count: 0, bytes: 0 } }),
    );
    expect(actions.map((a) => a.key)).toEqual(['downloads', 'history', 'sign-out']);
    expect(actions.filter((a) => a.row.hidden).map((a) => a.key)).toEqual(['downloads']);
  });

  it('keeps the downloads row and names leftover files when bytes remain with no ready track', () => {
    const [downloads] = buildDangerZoneActions(makeOpts({ downloads: { count: 0 } }));
    expect(downloads?.row.hidden).toBe(false);
    expect(downloads?.confirm.body).toBe(
      'Leftover download files (4 MB) will be deleted from this device.',
    );
  });

  it('uses the singular track noun in the remove-downloads body', () => {
    const [downloads] = buildDangerZoneActions(makeOpts());
    expect(downloads?.confirm.body.startsWith('1 track (4 MB)')).toBe(true);
  });

  it('marks a failed clear-history row with danger copy instead of Cleared', () => {
    const history = (clearHistory: Partial<ClearHistoryState>) =>
      buildDangerZoneActions(makeOpts({ clearHistory }))[1]?.row;
    const idle = history({ isPending: false, isSuccess: false, isError: false });
    expect(idle?.status).toBeUndefined();
    expect(idle?.detail).toBeUndefined();
    expect(history({ isSuccess: true, isError: false })?.status).toEqual({
      label: 'Cleared',
      tone: 'success',
    });
    expect(
      history({ isSuccess: false, isError: true, error: new NetworkError('transport', 'offline') }),
    ).toMatchObject({
      status: { label: 'Failed', tone: 'danger' },
      detail: 'Could not reach the server — check your connection and try again.',
    });
  });

  it('leaves the downloads row untouched until a removal has run', () => {
    const [downloads] = buildDangerZoneActions(makeOpts());
    expect(downloads?.row.status).toBeUndefined();
    expect(downloads?.row.detail).toBe('Frees 4 MB · tracks stay in your library');
  });

  it('marks only a failed sign-out row with danger copy', () => {
    const signOutRow = (signOutState: SignOutResult) =>
      buildDangerZoneActions(makeOpts({ signOutState }))[2]?.row;
    for (const status of ['idle', 'loading', 'ok'] as const) {
      expect(signOutRow({ status })?.status).toBeUndefined();
      expect(signOutRow({ status })?.detail).toBeUndefined();
    }
    expect(
      signOutRow({ status: 'error', error: new NetworkError('transport', 'offline') }),
    ).toMatchObject({
      disabled: false,
      status: { label: 'Failed', tone: 'danger' },
      detail: 'Could not reach the server — check your connection and try again.',
    });
  });
});

describe('the danger-zone action key union', () => {
  // Compile-time guard: tsc fails if `key` widens back to a bare string, which is what
  // let a typo like 'donwloads' type-check and then silently open no confirm at all.
  it('refuses a mistyped key where an action key belongs', () => {
    const realKeys: DangerZoneActionKey[] = buildDangerZoneActions(makeOpts()).map(
      ({ key }) => key,
    );
    // @ts-expect-error 'donwloads' is a typo, not one of the three action keys
    const mistypedKey: DangerZoneActionKey = 'donwloads';

    expect(realKeys).not.toContain(mistypedKey);
  });
});

describe('the remove-downloads row after a remove-all pass (#1754)', () => {
  let warn: jest.SpyInstance;

  beforeEach(() => {
    usePinnedStore.setState({ entries: {}, queue: [], isWorking: false, lastUnpinAll: undefined });
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
  });

  it('reads as failed when a file survives its delete', () => {
    seedReadyDownload('t1');
    __fs.failNext('delete', new Error('file is locked'));

    const outcome = usePinnedStore.getState().unpinAll();

    expect(outcome).toBe('partial');
    expect(removeDownloadsRow()).toMatchObject({
      status: { label: 'Failed', tone: 'danger' },
      detail: "Some downloads couldn't be removed — try again.",
    });
  });

  it('keeps its plain detail when every file is deleted', () => {
    seedReadyDownload('t1');

    const outcome = usePinnedStore.getState().unpinAll();

    expect(outcome).toBe('all-removed');
    expect(removeDownloadsRow()?.status).toBeUndefined();
    expect(removeDownloadsRow()?.detail).toBe('Frees 4 MB · tracks stay in your library');
  });
});
