import { Text } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';
import { ModalFrame } from '@shared/ui/primitives/ModalFrame';

function renderFrame(variant: 'center' | 'sheet' | 'popover' = 'center') {
  const onClose = jest.fn();
  render(
    <ModalFrame
      visible
      onClose={onClose}
      closeAction="test.modal.close"
      closeLabel="Close test"
      testID="frame"
      variant={variant}
    >
      <Text>content</Text>
    </ModalFrame>,
  );
  return onClose;
}

describe('ModalFrame', () => {
  it('shows its children when visible', () => {
    renderFrame();
    expect(screen.getByText('content')).toBeTruthy();
  });

  it('renders nothing when hidden', () => {
    render(
      <ModalFrame visible={false} onClose={jest.fn()} closeAction="test.modal.close">
        <Text>content</Text>
      </ModalFrame>,
    );
    expect(screen.queryByText('content')).toBeNull();
  });

  it.each(['center', 'sheet', 'popover'] as const)(
    'closes when the %s scrim is tapped',
    (variant) => {
      const onClose = renderFrame(variant);
      fireEvent.press(screen.getByRole('button', { name: 'Close test' }));
      expect(onClose).toHaveBeenCalledTimes(1);
    },
  );

  it('closes on the Android back button', () => {
    const onClose = renderFrame();
    screen.getByTestId('frame').props.onRequestClose();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('labels the scrim "Close" by default', () => {
    render(
      <ModalFrame visible onClose={jest.fn()} closeAction="test.modal.close">
        <Text>content</Text>
      </ModalFrame>,
    );
    expect(screen.getByRole('button', { name: 'Close' })).toBeTruthy();
  });

  it('exposes the keyboard-avoiding body under the testID suffix', () => {
    renderFrame('sheet');
    expect(screen.getByTestId('frame-keyboard')).toBeTruthy();
  });
});
