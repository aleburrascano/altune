import type { Session } from '@supabase/supabase-js';

import type { PinnedEntry } from '@shared/offline/pinnedStore';
import { ApiError, NetworkError } from '@shared/api-client';
import { failureCopyForAction } from '../failureCopyForAction';
import { backfillActionLabel, backfillActionTone, backfillDetail } from '../hooks/backfillStatus';
import { accountEmail } from '../hooks/useAccountEmail';
import { downloadStats } from '../hooks/useDownloadStats';

jest.mock('@shared/offline/pinnedStore', () => ({
  formatBytes: jest.requireActual('@shared/offline/pinnedFiles').formatBytes,
}));
jest.mock('@shared/auth/useSession', () => ({}));

const entry = (trackId: string, status: PinnedEntry['status']): PinnedEntry =>
  ({ trackId, status }) as PinnedEntry;

describe('downloadStats — counts only ready entries', () => {
  it('reports no downloads when nothing is ready, hiding the size detail', () => {
    const stats = downloadStats({ a: entry('a', 'queued'), b: entry('b', 'failed') }, 0);
    expect(stats).toEqual({
      downloadCount: 0,
      downloadBytes: 0,
      downloadSize: '0 B',
      usageLabel: 'No downloads on this device',
      usageDetail: undefined,
    });
  });

  it('uses the singular noun for one ready track and shows the formatted size', () => {
    const stats = downloadStats({ a: entry('a', 'ready'), b: entry('b', 'downloading') }, 2048);
    expect(stats.downloadCount).toBe(1);
    expect(stats.usageLabel).toBe('1 track');
    expect(stats.usageDetail).toBe('2.0 KB');
  });

  it('uses the plural noun for several ready tracks', () => {
    const stats = downloadStats({ a: entry('a', 'ready'), b: entry('b', 'ready') }, 5 * 1024 ** 2);
    expect(stats.usageLabel).toBe('2 tracks');
    expect(stats.downloadSize).toBe('5.0 MB');
  });
});

describe('backfill status copy', () => {
  const base = { error: null, data: undefined };
  const idle = { ...base, status: 'idle' as const };

  it('says nothing and offers Run before the first run', () => {
    expect(backfillDetail(idle)).toBeUndefined();
    expect(backfillActionLabel(idle)).toBe('Run');
  });

  it('shows the resolving message while pending, even with stale data', () => {
    const pending = {
      ...base,
      status: 'pending' as const,
      data: { updated: 1, scanned: 2 },
    };
    expect(backfillDetail(pending)).toBe('Resolving featured artists…');
    expect(backfillActionLabel(pending)).toBe('Running…');
    expect(backfillActionTone(pending)).toBe('accent');
  });

  it('reports updated of scanned once done', () => {
    const done = { ...base, status: 'success' as const, data: { updated: 3, scanned: 40 } };
    expect(backfillDetail(done)).toBe('Updated 3 of 40 tracks');
    expect(backfillActionLabel(done)).toBe('Done');
    expect(backfillActionTone(done)).toBe('success');
  });

  it('reports a failure distinctly from idle and done, in the danger tone', () => {
    const failed = {
      ...base,
      status: 'error' as const,
      error: new NetworkError('transport', 'offline'),
    };
    expect(backfillDetail(failed)).toBe(
      'Could not reach the server — check your connection and try again.',
    );
    expect(backfillActionLabel(failed)).toBe('Retry');
    expect(backfillActionTone(failed)).toBe('danger');
    expect(backfillActionTone(idle)).toBe('accent');
  });
});

describe('failureCopyForAction', () => {
  it('maps network, auth, server and unknown failures to distinct copy', () => {
    const copy = [
      new NetworkError('timeout', 't'),
      new ApiError(401, 'x'),
      new ApiError(403, 'x'),
      new ApiError(502, 'x'),
      new ApiError(404, 'x'),
      new Error('boom'),
    ].map(failureCopyForAction);
    expect(copy).toEqual([
      'Could not reach the server — check your connection and try again.',
      'Your session has expired — sign in again and retry.',
      'Your session has expired — sign in again and retry.',
      'The server had a problem — try again in a few minutes.',
      'Something went wrong — try again.',
      'Something went wrong — try again.',
    ]);
  });
});

describe('accountEmail', () => {
  const signedIn = (email: string | undefined) => ({
    status: 'signed-in' as const,
    session: { user: { email } } as Session,
  });

  it('is the signed-in user email', () => {
    expect(accountEmail(signedIn('me@example.com'))).toBe('me@example.com');
  });

  it('is empty when the user has no email or nobody is signed in', () => {
    expect(accountEmail(signedIn(undefined))).toBe('');
    expect(accountEmail({ status: 'loading' })).toBe('');
    expect(accountEmail({ status: 'signed-out' })).toBe('');
  });
});
