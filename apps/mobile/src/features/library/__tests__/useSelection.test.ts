import { act, renderHook } from '@testing-library/react-native';

import { asTrackId, type TrackId } from '@shared/api-client/ids';

import { useSelection } from '../hooks/useSelection';

describe('useSelection', () => {
  it('starts inactive, so a list renders no checkboxes until a selection begins', () => {
    const { result } = renderHook(() => useSelection());

    expect(result.current.active).toBe(false);
    expect(result.current.ids).toEqual([]);
    expect(result.current.count).toBe(0);
  });

  it('begin enters selection mode with that one id selected', () => {
    const { result } = renderHook(() => useSelection());

    act(() => result.current.begin(asTrackId('t1')));

    expect(result.current.active).toBe(true);
    expect(result.current.ids).toEqual(['t1']);
    expect(result.current.has(asTrackId('t1'))).toBe(true);
  });

  it('begin on an already-active selection keeps what is selected rather than resetting it', () => {
    const { result } = renderHook(() => useSelection());

    act(() => result.current.begin(asTrackId('t1')));
    act(() => result.current.toggle(asTrackId('t2')));
    act(() => result.current.begin(asTrackId('t3')));

    expect(result.current.ids).toEqual(['t1', 't2']);
  });

  it('toggle adds in tap order and removes on a second tap', () => {
    const { result } = renderHook(() => useSelection());

    act(() => result.current.begin(asTrackId('t1')));
    act(() => result.current.toggle(asTrackId('t3')));
    act(() => result.current.toggle(asTrackId('t2')));
    expect(result.current.ids).toEqual(['t1', 't3', 't2']);

    act(() => result.current.toggle(asTrackId('t3')));
    expect(result.current.ids).toEqual(['t1', 't2']);
  });

  it('leaves selection mode when the last selected id is deselected', () => {
    const { result } = renderHook(() => useSelection());

    act(() => result.current.begin(asTrackId('t1')));
    act(() => result.current.toggle(asTrackId('t1')));

    expect(result.current.active).toBe(false);
    expect(result.current.count).toBe(0);
  });

  it('toggle on a fresh hook enters selection mode, so a row press cannot select into a null set', () => {
    const { result } = renderHook(() => useSelection());

    act(() => result.current.toggle(asTrackId('t1')));

    expect(result.current.active).toBe(true);
    expect(result.current.ids).toEqual(['t1']);
  });

  it('selectAll replaces the selection with exactly the ids given', () => {
    const { result } = renderHook(() => useSelection());

    act(() => result.current.begin(asTrackId('t9')));
    act(() => result.current.selectAll([asTrackId('t1'), asTrackId('t2'), asTrackId('t3')]));

    expect(result.current.ids).toEqual(['t1', 't2', 't3']);
    expect(result.current.has(asTrackId('t9'))).toBe(false);
  });

  it('selectAll with an empty list stays active with nothing selected rather than exiting', () => {
    const { result } = renderHook(() => useSelection());

    act(() => result.current.begin(asTrackId('t1')));
    act(() => result.current.selectAll([]));

    expect(result.current.active).toBe(true);
    expect(result.current.count).toBe(0);
  });

  it('clear exits selection mode', () => {
    const { result } = renderHook(() => useSelection());

    act(() => result.current.begin(asTrackId('t1')));
    act(() => result.current.toggle(asTrackId('t2')));
    act(() => result.current.clear());

    expect(result.current.active).toBe(false);
    expect(result.current.ids).toEqual([]);
  });

  it('hands out a copy, so mutating the returned array cannot corrupt the selection', () => {
    const { result } = renderHook(() => useSelection());

    act(() => result.current.begin(asTrackId('t1')));
    result.current.ids.push(asTrackId('t2'));

    expect(result.current.has(asTrackId('t2'))).toBe(false);
    act(() => result.current.toggle(asTrackId('t3')));
    expect(result.current.ids).toEqual(['t1', 't3']);
  });

  it('toggling the same id twice in one batch is a no-op, not a double add', () => {
    const { result } = renderHook(() => useSelection());

    act(() => {
      result.current.toggle(asTrackId('t1'));
    });
    act(() => {
      result.current.toggle(asTrackId('t1'));
    });

    expect(result.current.active).toBe(false);
  });
});

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
