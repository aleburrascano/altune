import { Alert } from 'react-native';

import { confirmDestructive } from '../confirmDestructive';

describe('confirmDestructive (native)', () => {
  it('shows a cancel and a destructive button whose press runs onConfirm', () => {
    jest.spyOn(Alert, 'alert').mockImplementation(() => {});
    const onConfirm = jest.fn();

    confirmDestructive({
      title: 'Delete playlist',
      message: 'Are you sure?',
      confirmLabel: 'Delete',
      onConfirm,
    });

    expect(Alert.alert).toHaveBeenCalledTimes(1);
    const [title, message, buttons] = jest.mocked(Alert.alert).mock.calls[0]!;
    expect(title).toBe('Delete playlist');
    expect(message).toBe('Are you sure?');
    expect(buttons?.find((button) => button.style === 'cancel')).toBeDefined();
    const destructiveButton = buttons?.find((button) => button.style === 'destructive');
    expect(destructiveButton?.text).toBe('Delete');
    expect(onConfirm).not.toHaveBeenCalled();
    destructiveButton?.onPress?.();
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });
});
