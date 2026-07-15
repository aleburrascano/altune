import {
  useDownloadStore,
  startDownload,
  progressDownload,
  completeDownload,
  failDownload,
  aggregatePhase,
  FINISHING_DWELL_MS,
  DONE_HOLD_MS,
  FAILED_HOLD_MS,
  type DownloadEntry,
} from '../downloadStore';

beforeEach(() => {
  useDownloadStore.getState().reset();
});

const entries = (): Record<string, DownloadEntry> => useDownloadStore.getState().entries;
const phaseOf = (id: string): string | undefined => entries()[id]?.phase;

describe('downloadStore lifecycle', () => {
  it('seeds an entry at finding on start, with snapshotted meta', () => {
    startDownload('t1', { title: 'Song', artist: 'Artist', artworkUrl: 'art' });
    expect(entries()['t1']).toMatchObject({
      trackId: 't1',
      phase: 'finding',
      title: 'Song',
      artist: 'Artist',
      artworkUrl: 'art',
    });
  });

  it('advances phase on progress and keeps meta once set', () => {
    startDownload('t1', { title: 'Song', artist: 'Artist', artworkUrl: 'art' });
    progressDownload('t1', 'downloading');
    expect(phaseOf('t1')).toBe('downloading');
    expect(entries()['t1']).toMatchObject({ title: 'Song', artist: 'Artist' });
  });

  it('does not regress the phase on an out-of-order progress event', () => {
    startDownload('t1');
    progressDownload('t1', 'finishing');
    progressDownload('t1', 'finding'); // late, out of order
    expect(phaseOf('t1')).toBe('finishing');
  });

  it('creates an entry from progress if start was missed', () => {
    progressDownload('t1', 'downloading', { title: 'Late', artist: 'A', artworkUrl: null });
    expect(entries()['t1']).toMatchObject({ phase: 'downloading', title: 'Late' });
  });

  it('holds finishing then done then removes on complete', () => {
    jest.useFakeTimers();
    startDownload('t1');
    progressDownload('t1', 'downloading');

    completeDownload('t1');
    expect(phaseOf('t1')).toBe('finishing'); // finishing paints first, not gone

    jest.advanceTimersByTime(FINISHING_DWELL_MS);
    expect(phaseOf('t1')).toBe('done'); // then the done ✓ tail

    jest.advanceTimersByTime(DONE_HOLD_MS);
    expect(entries()['t1']).toBeUndefined(); // then removed

    jest.useRealTimers();
  });

  it('marks failed then removes after the hold', () => {
    jest.useFakeTimers();
    startDownload('t1');
    failDownload('t1');
    expect(phaseOf('t1')).toBe('failed');

    jest.advanceTimersByTime(FAILED_HOLD_MS);
    expect(entries()['t1']).toBeUndefined();

    jest.useRealTimers();
  });

  it('ignores a stale progress after a terminal state', () => {
    jest.useFakeTimers();
    startDownload('t1');
    completeDownload('t1'); // -> finishing (terminal sequence running)
    jest.advanceTimersByTime(FINISHING_DWELL_MS);
    expect(phaseOf('t1')).toBe('done');
    progressDownload('t1', 'downloading'); // stale, must not revive
    expect(phaseOf('t1')).toBe('done');
    jest.useRealTimers();
  });

  it('re-acquire (start after done) cancels the pending removal and restarts', () => {
    jest.useFakeTimers();
    startDownload('t1');
    completeDownload('t1');
    jest.advanceTimersByTime(FINISHING_DWELL_MS); // now 'done', removal pending

    startDownload('t1'); // re-acquire before removal fires
    expect(phaseOf('t1')).toBe('finding');

    jest.advanceTimersByTime(DONE_HOLD_MS + FINISHING_DWELL_MS); // old timers must be cancelled
    expect(phaseOf('t1')).toBe('finding'); // still present, not removed by the stale timer

    jest.useRealTimers();
  });
});

describe('aggregatePhase (F9)', () => {
  const e = (id: string, phase: DownloadEntry['phase']): DownloadEntry => ({
    trackId: id,
    phase,
    title: null,
    artist: null,
    artworkUrl: null,
  });

  it('returns undefined for an empty list', () => {
    expect(aggregatePhase([])).toBeUndefined();
  });

  it('returns the least-advanced active phase', () => {
    expect(aggregatePhase([e('a', 'finishing'), e('b', 'downloading'), e('c', 'finding')])).toBe(
      'finding',
    );
  });

  it('ignores done items unless all are done', () => {
    expect(aggregatePhase([e('a', 'done'), e('b', 'downloading')])).toBe('downloading');
    expect(aggregatePhase([e('a', 'done'), e('b', 'done')])).toBe('done');
  });
});
