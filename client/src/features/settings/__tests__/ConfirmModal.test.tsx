import { StyleSheet } from 'react-native';
import { render, screen } from '@testing-library/react-native';

import { ModalFrame } from '@shared/ui';

describe('ModalFrame', () => {
  it('pads the keyboard-avoiding view on iOS', () => {
    render(
      <ModalFrame
        visible
        onClose={() => undefined}
        closeAction="settings.modal.close"
        testID="modal"
      >
        {null}
      </ModalFrame>,
    );

    expect(StyleSheet.flatten(screen.getByTestId('modal-keyboard').props.style)).toHaveProperty(
      'paddingBottom',
      0,
    );
  });
});
