import React from 'react';
import { StyleSheet } from 'react-native';
import { act, fireEvent, render, screen } from '@testing-library/react-native';

import { Scrubber } from '../Scrubber';

// A single active touch at `pageX`, shaped like the responder system's touch history
// so PanResponder's own grant/move/release bookkeeping runs for real.
function touchEvent(pageX: number, timeStamp: number) {
  const touch = {
    touchActive: true,
    startPageX: pageX,
    startPageY: 0,
    startTimeStamp: timeStamp,
    currentPageX: pageX,
    currentPageY: 0,
    currentTimeStamp: timeStamp,
    previousPageX: pageX,
    previousPageY: 0,
    previousTimeStamp: timeStamp,
  };
  return {
    nativeEvent: { pageX, pageY: 0, touches: [], changedTouches: [] },
    touchHistory: {
      numberActiveTouches: 1,
      indexOfSingleActiveTouch: 0,
      mostRecentTimeStamp: timeStamp,
      touchBank: [touch],
    },
  };
}

function dragAndRelease(pageX: number) {
  const track = screen.getByLabelText(/^Playback position/);
  act(() => {
    fireEvent(track, 'responderGrant', touchEvent(pageX, 1));
    fireEvent(track, 'responderMove', touchEvent(pageX, 2));
    fireEvent(track, 'responderRelease', touchEvent(pageX, 3));
  });
}

// The fill is the only view whose width is an animated percentage of the track.
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

    // The unmeasured track clamps any far-right drag to the end.
    dragAndRelease(9999);

    expect(onSeek).toHaveBeenCalledWith(200000);
  });
});
