import { StyleSheet, Text } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import {
  DismissKeyboardArea,
  KeepInputFocusArea,
  ModalFrame,
  adjustsKeyboardInsets,
  dismissKeyboard,
} from '@shared/ui';

describe('keyboard (native)', () => {
  it('DismissKeyboardArea hands a press to the dismissal it was given', () => {
    const onDismiss = jest.fn();
    render(
      <DismissKeyboardArea testID="area" onDismiss={onDismiss}>
        <Text>child</Text>
      </DismissKeyboardArea>,
    );

    fireEvent.press(screen.getByTestId('area'));

    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('DismissKeyboardArea does not throw on press when it is given no dismissal', () => {
    render(
      <DismissKeyboardArea testID="area">
        <Text>child</Text>
      </DismissKeyboardArea>,
    );

    expect(() => fireEvent.press(screen.getByTestId('area'))).not.toThrow();
  });

  it('a press on KeepInputFocusArea content passes through to the surrounding dismiss area', () => {
    const onDismiss = jest.fn();
    render(
      <DismissKeyboardArea testID="outer" onDismiss={onDismiss}>
        <KeepInputFocusArea testID="area">
          <Text>child</Text>
        </KeepInputFocusArea>
      </DismissKeyboardArea>,
    );

    fireEvent.press(screen.getByText('child'));

    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('dismissKeyboard does not throw', () => {
    expect(() => dismissKeyboard()).not.toThrow();
  });

  it('adjusts keyboard insets on the native platform the tests run as', () => {
    expect(adjustsKeyboardInsets).toBe(true);
  });

  it('a modal avoids the keyboard with padding by default on the native platform', () => {
    render(
      <ModalFrame visible onClose={jest.fn()} closeAction="test.modal.close" testID="frame">
        <Text>content</Text>
      </ModalFrame>,
    );

    expect(StyleSheet.flatten(screen.getByTestId('frame-keyboard').props.style)).toHaveProperty(
      'paddingBottom',
      0,
    );
  });
});
