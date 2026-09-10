import { act, renderHook } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';

import { useSelection } from '../useSelection';

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
