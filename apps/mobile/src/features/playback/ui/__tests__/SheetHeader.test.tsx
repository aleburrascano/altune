import React from 'react';
import { StyleSheet, Text } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { SheetHeader, SheetHeaderCenter, SheetHeaderTrailing, SheetScreen } from '../SheetHeader';

jest.mock('react-native-safe-area-context', () => ({
  useSafeAreaInsets: () => ({ top: 37, bottom: 0, left: 0, right: 0 }),
}));

function renderSheet(onClose = jest.fn()) {
  render(
    <SheetScreen testID="sheet">
      <SheetHeader onClose={onClose} closeLabel="Close queue">
        <SheetHeaderCenter>
          <Text>Up Next</Text>
        </SheetHeaderCenter>
        <SheetHeaderTrailing>
          <Text>Clear</Text>
        </SheetHeaderTrailing>
      </SheetHeader>
      <Text>body</Text>
    </SheetScreen>,
  );
  return onClose;
}

describe('SheetScreen / SheetHeader', () => {
  it('closes through the labelled chevron', () => {
    const onClose = renderSheet();
    fireEvent.press(screen.getByLabelText('Close queue'));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('renders the center, trailing and body slots', () => {
    renderSheet();
    expect(screen.getByText('Up Next')).toBeTruthy();
    expect(screen.getByText('Clear')).toBeTruthy();
    expect(screen.getByText('body')).toBeTruthy();
  });

  it('pads the container below the top safe-area inset', () => {
    renderSheet();
    const style = StyleSheet.flatten(screen.getByTestId('sheet').props.style);
    expect(style.paddingTop).toBe(37);
    expect(style.flex).toBe(1);
  });
});
