import React from 'react';
import { StyleSheet } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { darkTheme } from '@shared/ui/theme';

import { CorrectionBanner } from '../ui/CorrectionBanner';
import { DiscoverRow } from '../ui/DiscoverRow';
import { resultFixture } from './fixtures';

jest.mock('../hooks/usePreviewPlayback', () => ({
  usePreviewPlayback: () => ({ hasPreview: false }),
}));

// Pressable's `pressed` comes from the touch responder, not from a prop, and
// fireEvent.press() grants and releases in one go — so hold the responder open
// by hand to observe the held-down style.
function holdDown(element: Parameters<typeof fireEvent>[0]): void {
  fireEvent(element, 'responderGrant', {
    persist: () => {},
    nativeEvent: {
      touches: [],
      changedTouches: [],
      identifier: 1,
      locationX: 0,
      locationY: 0,
      pageX: 0,
      pageY: 0,
      timestamp: Date.now(),
    },
    currentTarget: { measure: () => {} },
  });
}

function styleOf(testID: string): { opacity?: number; backgroundColor?: string } {
  return StyleSheet.flatten(screen.getByTestId(testID).props.style) ?? {};
}

describe('discover Pressables share one press feedback', () => {
  it('dims a plain discover Pressable while it is held down', () => {
    render(
      <CorrectionBanner
        correctedQuery="radiohead"
        originalQuery="radiohed"
        onSearchOriginal={() => {}}
      />,
    );
    const link = screen.getByLabelText('Search instead for radiohed');

    holdDown(link);

    expect(StyleSheet.flatten(link.props.style).opacity).toBe(0.7);
  });

  it('leaves a plain discover Pressable at full opacity when it is not held down', () => {
    render(
      <CorrectionBanner
        correctedQuery="radiohead"
        originalQuery="radiohed"
        onSearchOriginal={() => {}}
      />,
    );

    const link = screen.getByLabelText('Search instead for radiohed');

    expect(StyleSheet.flatten(link.props.style).opacity).toBeUndefined();
  });

  it('tints a held-down result row on top of the shared dim', () => {
    render(<DiscoverRow result={resultFixture()} position={0} onPress={() => {}} />);

    holdDown(screen.getByTestId('discover-row-track-0'));

    expect(styleOf('discover-row-track-0')).toMatchObject({
      opacity: 0.7,
      backgroundColor: darkTheme.color.surface1,
    });
  });

  it('leaves a result row untinted and at full opacity when it is not held down', () => {
    render(<DiscoverRow result={resultFixture()} position={0} onPress={() => {}} />);

    const style = styleOf('discover-row-track-0');

    expect(style.opacity).toBeUndefined();
    expect(style.backgroundColor).toBeUndefined();
  });
});
