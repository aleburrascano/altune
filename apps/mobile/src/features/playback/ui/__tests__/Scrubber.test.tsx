import React from 'react';
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
