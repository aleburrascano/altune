import { act, renderHook } from '@testing-library/react-native';

import { asTrackId, type TrackId } from '@shared/api-client/ids';

import { useSelection } from '../hooks/useSelection';

// A selection built one row at a time, and the size one can already have reached in a large
// library. A toggle that scans the selection costs roughly nine times more per tap on the
// crowded run (~17000 ids held on average) than on the empty one (~1000).
const TAPS = 2000;
const CROWDED = 16000;

// The crowded run does one extra whole-selection copy when the batch renders, and the runner's
// scheduler adds noise on top; three times leaves room for both while still failing anything
// that walks the selection per tap.
const TOLERATED_GROWTH = 3;

function ids(prefix: string, count: number): TrackId[] {
  return Array.from({ length: count }, (_, i) => asTrackId(`${prefix}${i}`));
}

// jsdom rounds performance.now() to whole milliseconds, which is coarser than a run of cheap
// taps takes; the process clock is what can tell one from three.
function nowMs(): number {
  return Number(process.hrtime.bigint()) / 1e6;
}

// Milliseconds per tap for TAPS individual row taps onto a selection already holding
// `held` ids. The taps share one act, so what is timed is the toggles themselves plus the
// single render they batch into, rather than one render per tap.
function msPerTap(held: number): number {
  const { result } = renderHook(() => useSelection());
  if (held > 0) act(() => result.current.selectAll(ids('held', held)));
  const tapped = ids('tap', TAPS);

  const startedAt = nowMs();
  act(() => {
    for (const id of tapped) result.current.toggle(id);
  });

  return (nowMs() - startedAt) / TAPS;
}

// The fastest of three runs: a slow run can only come from noise, never from the hook being
// cheaper than it is, so the minimum is the measurement least able to flake.
function fastestMsPerTap(held: number): number {
  return Math.min(msPerTap(held), msPerTap(held), msPerTap(held));
}

describe('building a selection one row tap at a time', () => {
  it('costs the same per tap on a crowded selection as on an empty one, so N taps stay linear in N', () => {
    const onEmpty = fastestMsPerTap(0);

    const onCrowded = fastestMsPerTap(CROWDED);

    expect(onCrowded).toBeLessThanOrEqual(onEmpty * TOLERATED_GROWTH);
  });

  it('keeps every tapped id, so the cheap toggle still selects what was tapped', () => {
    const { result } = renderHook(() => useSelection());
    act(() => result.current.selectAll(ids('held', CROWDED)));

    act(() => {
      for (const id of ids('tap', TAPS)) result.current.toggle(id);
    });

    expect(result.current.count).toBe(CROWDED + TAPS);
    expect(result.current.has(asTrackId('tap1999'))).toBe(true);
    expect(result.current.has(asTrackId('held0'))).toBe(true);
  });
});
