import React from 'react';
import { StyleSheet } from 'react-native';
import { act, render, screen } from '@testing-library/react-native';
import { State } from 'react-native-gesture-handler';
import { fireGestureHandler, getByGestureTestId } from 'react-native-gesture-handler/jest-utils';

import { Scrubber } from '@features/playback/ui/Scrubber';

function dragAndRelease(pageX: number) {
  const pan = getByGestureTestId('scrubber-pan');
  act(() => {
    fireGestureHandler(pan, [
      { state: State.BEGAN, absoluteX: pageX },
      { state: State.ACTIVE, absoluteX: pageX },
      { state: State.ACTIVE, absoluteX: pageX },
      { state: State.END, absoluteX: pageX },
    ]);
  });
}

function fillWidthPercent(): number {
  const track = screen.getByLabelText(/^Playback position/);
  const widths = track
    .findAll((node) => typeof node.type === 'string')
    .map((node) => (StyleSheet.flatten(node.props.style) as { width?: unknown } | undefined)?.width)
    .filter((width): width is string => typeof width === 'string' && width.endsWith('%'));
  return parseFloat(String(widths[0]));
}

describe('Scrubber progress bounds', () => {
  it('never fills past the track end when the position overshoots the duration', () => {
    render(<Scrubber positionMs={210000} durationMs={200000} onSeek={jest.fn()} />);

    expect(fillWidthPercent()).toBe(100);
  });

  it('never fills before the track start when the position is negative', () => {
    render(<Scrubber positionMs={-5000} durationMs={200000} onSeek={jest.fn()} />);

    expect(fillWidthPercent()).toBe(0);
  });

  it('fills proportionally within the track', () => {
    render(<Scrubber positionMs={50000} durationMs={200000} onSeek={jest.fn()} />);

    expect(fillWidthPercent()).toBe(25);
  });
});

describe('Scrubber drag seeking', () => {
  it('does not seek when dragged before the duration is known', () => {
    const onSeek = jest.fn();
    render(<Scrubber positionMs={0} durationMs={0} onSeek={onSeek} />);

    dragAndRelease(9999);

    expect(onSeek).not.toHaveBeenCalled();
  });

  it('seeks to the dragged position once the duration is known', () => {
    const onSeek = jest.fn();
    render(<Scrubber positionMs={0} durationMs={200000} onSeek={onSeek} />);

    dragAndRelease(9999);

    expect(onSeek).toHaveBeenCalledWith(200000);
  });
});
