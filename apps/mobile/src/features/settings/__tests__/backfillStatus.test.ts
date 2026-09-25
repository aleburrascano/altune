import { NetworkError } from '@shared/api-client';
import { backfillActionLabel, backfillActionTone, backfillDetail } from '../hooks/backfillStatus';

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
