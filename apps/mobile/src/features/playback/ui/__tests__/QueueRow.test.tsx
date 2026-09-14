import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { formatTime, type QueueItem } from '../../queueItem';
import { QueueRow } from '../QueueRow';

// Native worklets cannot load under jest; the swipe container only needs to render its row.
jest.mock('react-native-reanimated', () => ({
  __esModule: true,
  default: { View: jest.requireActual('react-native').View },
  useAnimatedStyle: () => ({}),
}));
jest.mock('react-native-gesture-handler/ReanimatedSwipeable', () => ({
  __esModule: true,
  default: ({ children }: { children: React.ReactNode }) => children,
}));

const item: QueueItem = {
  trackIndex: 4,
  queueIndex: 6,
  title: 'Song',
  artist: 'Band',
  artworkUrl: null,
  durationSeconds: 185,
  featuredArtists: undefined,
};

function renderRow() {
  const props = { onSkip: jest.fn(), onRemove: jest.fn(), onOpenMenu: jest.fn() };
  render(<QueueRow item={item} {...props} />);
  return props;
}

describe('QueueRow', () => {
  it('skips to its queue index when pressed', () => {
    const props = renderRow();
    fireEvent.press(screen.getByLabelText('Song by Band'));
    expect(props.onSkip).toHaveBeenCalledWith(6);
    expect(props.onOpenMenu).not.toHaveBeenCalled();
  });

  it('opens the menu for its item from the options button', () => {
    const props = renderRow();
    fireEvent.press(screen.getByLabelText('Options for Song'));
    expect(props.onOpenMenu).toHaveBeenCalledWith(item);
  });

  it('shows the formatted duration', () => {
    renderRow();
    expect(screen.getByText('3:05')).toBeTruthy();
  });
});

describe('formatTime', () => {
  it('renders nothing for missing or zero durations', () => {
    expect(formatTime(undefined)).toBe('');
    expect(formatTime(0)).toBe('');
  });

  it('pads seconds and floors fractions', () => {
    expect(formatTime(61.9)).toBe('1:01');
  });
});
