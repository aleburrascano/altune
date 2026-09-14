import type { SignOutResult } from '@shared/auth/useSignOut';

import type { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { buildDangerZoneActions } from '../ui/dangerZoneActions';

type Opts = Parameters<typeof buildDangerZoneActions>[0];

function makeOpts(over: Partial<Opts> = {}): Opts {
  return {
    downloadCount: 1,
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
    const actions = buildDangerZoneActions(makeOpts({ downloadCount: 0 }));
    expect(actions.map((a) => a.key)).toEqual(['downloads', 'history', 'sign-out']);
    expect(actions.filter((a) => a.row.hidden).map((a) => a.key)).toEqual(['downloads']);
  });

  it('uses the singular track noun in the remove-downloads body', () => {
    const [downloads] = buildDangerZoneActions(makeOpts());
    expect(downloads?.confirm.body.startsWith('1 track (4 MB)')).toBe(true);
  });
});
