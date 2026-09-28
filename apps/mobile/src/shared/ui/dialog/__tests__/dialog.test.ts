import { Alert } from 'react-native';

import { confirm, showAlert } from '../dialog';
import * as webDialog from '../dialog.web';

describe('dialog (native)', () => {
  it('confirm() shows a cancel button and a destructive button running onConfirm', () => {
    jest.spyOn(Alert, 'alert').mockImplementation(() => {});
    const onConfirm = jest.fn();

    confirm({
      title: 'Delete playlist',
      message: 'Are you sure?',
      confirmLabel: 'Delete',
      onConfirm,
    });

    expect(Alert.alert).toHaveBeenCalledTimes(1);
    const [title, message, buttons] = jest.mocked(Alert.alert).mock.calls[0]!;
    expect(title).toBe('Delete playlist');
    expect(message).toBe('Are you sure?');
    const cancelButton = buttons?.find((button) => button.style === 'cancel');
    const destructiveButton = buttons?.find((button) => button.style === 'destructive');
    expect(cancelButton).toBeDefined();
    expect(destructiveButton?.text).toBe('Delete');

    expect(onConfirm).not.toHaveBeenCalled();
    destructiveButton?.onPress?.();
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('showAlert() shows the title and message', () => {
    jest.spyOn(Alert, 'alert').mockImplementation(() => {});

    showAlert('Download failed', 'Not enough storage');

    expect(Alert.alert).toHaveBeenCalledWith('Download failed', 'Not enough storage');
  });
});

describe('dialog.web', () => {
  beforeEach(() => {
    window.confirm = jest.fn();
    window.alert = jest.fn();
  });

  it('confirm() calls window.confirm with the title and message, and runs onConfirm only when it returns true', () => {
    const confirmSpy = jest.spyOn(window, 'confirm').mockReturnValue(true);
    const onConfirm = jest.fn();

    webDialog.confirm({
      title: 'Delete playlist',
      message: 'Are you sure?',
      confirmLabel: 'Delete',
      onConfirm,
    });

    expect(confirmSpy).toHaveBeenCalledWith('Delete playlist\n\nAre you sure?');
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('confirm() never runs onConfirm when window.confirm returns false', () => {
    jest.spyOn(window, 'confirm').mockReturnValue(false);
    const onConfirm = jest.fn();

    webDialog.confirm({
      title: 'Delete playlist',
      message: 'Are you sure?',
      confirmLabel: 'Delete',
      onConfirm,
    });

    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('showAlert() calls window.alert with the title and message', () => {
    const alertSpy = jest.spyOn(window, 'alert').mockImplementation(() => {});

    webDialog.showAlert('Download failed', 'Not enough storage');

    expect(alertSpy).toHaveBeenCalledWith('Download failed\n\nNot enough storage');
  });
});

describe('dialog (native) edge cases', () => {
  it('confirm() offers exactly two buttons and cancelling never runs onConfirm', () => {
    jest.spyOn(Alert, 'alert').mockImplementation(() => {});
    const onConfirm = jest.fn();

    confirm({
      title: 'Remove track',
      message: 'Remove from playlist?',
      confirmLabel: 'Remove',
      onConfirm,
    });

    const buttons = jest.mocked(Alert.alert).mock.calls.at(-1)![2]!;
    expect(buttons).toHaveLength(2);
    const cancelButton = buttons.find((button) => button.style === 'cancel');
    cancelButton?.onPress?.();
    expect(onConfirm).not.toHaveBeenCalled();
  });
});

describe('dialog.web edge cases', () => {
  beforeEach(() => {
    window.confirm = jest.fn();
    window.alert = jest.fn();
  });

  it('confirm() asks the user once and waits for the answer before running onConfirm', () => {
    const onConfirm = jest.fn();
    const confirmSpy = jest.spyOn(window, 'confirm').mockImplementation(() => {
      expect(onConfirm).not.toHaveBeenCalled();
      return true;
    });

    webDialog.confirm({
      title: 'Delete playlist',
      message: 'Are you sure?',
      confirmLabel: 'Delete',
      onConfirm,
    });

    expect(confirmSpy).toHaveBeenCalledTimes(1);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('confirm() runs onConfirm once per accepted prompt when called twice', () => {
    jest.spyOn(window, 'confirm').mockReturnValueOnce(true).mockReturnValueOnce(false);
    const onConfirm = jest.fn();
    const options = {
      title: 'Delete playlist',
      message: 'Are you sure?',
      confirmLabel: 'Delete',
      onConfirm,
    };

    webDialog.confirm(options);
    webDialog.confirm(options);

    expect(window.confirm).toHaveBeenCalledTimes(2);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });
});
