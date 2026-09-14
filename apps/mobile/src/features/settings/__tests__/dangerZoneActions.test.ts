import { NetworkError } from '@shared/api-client';
import type { SignOutResult } from '@shared/auth/useSignOut';

import type { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { buildDangerZoneActions } from '../ui/dangerZoneActions';

type Opts = Parameters<typeof buildDangerZoneActions>[0];

function makeOpts(over: Partial<Opts> = {}): Opts {
  return {
    downloadCount: 1,
    downloadBytes: 4 * 1024 ** 2,
    downloadSize: '4 MB',
    signOutState: { kind: 'idle' } as SignOutResult,
    clearHistory: {
      mutate: jest.fn(),
      isPending: false,
      isSuccess: false,
    } as unknown as ReturnType<typeof useClearSearchHistory>,
    unpinAll: jest.fn(),
    signOut: jest.fn().mockResolvedValue(undefined),
    ...over,
  };
}

describe('buildDangerZoneActions', () => {
  it('lists downloads, history, sign-out in that order with distinct confirm testIDs', () => {
    const actions = buildDangerZoneActions(makeOpts());
    expect(actions.map((a) => a.key)).toEqual(['downloads', 'history', 'sign-out']);
    expect(new Set(actions.map((a) => a.confirm.testID)).size).toBe(3);
  });

  it('hides only the downloads row when nothing is downloaded', () => {
    const actions = buildDangerZoneActions(makeOpts({ downloadCount: 0, downloadBytes: 0 }));
    expect(actions.map((a) => a.key)).toEqual(['downloads', 'history', 'sign-out']);
    expect(actions.filter((a) => a.row.hidden).map((a) => a.key)).toEqual(['downloads']);
  });

  it('keeps the downloads row and names leftover files when bytes remain with no ready track', () => {
    const [downloads] = buildDangerZoneActions(makeOpts({ downloadCount: 0 }));
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
    const history = (clearHistory: object) =>
      buildDangerZoneActions(
        makeOpts({
          clearHistory: clearHistory as unknown as ReturnType<typeof useClearSearchHistory>,
        }),
      )[1]?.row;
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

  it('marks only a failed sign-out row with danger copy', () => {
    const signOutRow = (signOutState: SignOutResult) =>
      buildDangerZoneActions(makeOpts({ signOutState }))[2]?.row;
    for (const kind of ['idle', 'pending', 'ok'] as const) {
      expect(signOutRow({ kind })?.status).toBeUndefined();
      expect(signOutRow({ kind })?.detail).toBeUndefined();
    }
    expect(signOutRow({ kind: 'error' })).toMatchObject({
      disabled: false,
      status: { label: 'Failed', tone: 'danger' },
      detail: 'Could not sign out — check your connection and try again.',
    });
  });
});
