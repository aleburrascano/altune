import { Alert } from 'react-native';

import { confirmDestructive } from '../confirmDestructive';

describe('confirmDestructive (native)', () => {
  it('shows a cancel and a destructive button whose press runs onConfirm', () => {
    jest.spyOn(Alert, 'alert').mockImplementation(() => {});
    const onConfirm = jest.fn();

    confirmDestructive({ title: 'Delete playlist', message: 'Are you sure?', confirmLabel: 'Delete', onConfirm });

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

describe('confirmDestructive (web)', () => {
  beforeEach(() => {
    jest.resetModules();
    jest.doMock('../dialog/dialog', () => jest.requireActual('../dialog/dialog.web'));
    window.confirm = jest.fn();
  });

  afterEach(() => {
    jest.dontMock('../dialog/dialog');
  });

  it('asks window.confirm with the title and message and runs onConfirm once when accepted', () => {
    const { confirmDestructive: webConfirmDestructive } = require('../confirmDestructive') as {
      confirmDestructive: typeof confirmDestructive;
    };
    jest.spyOn(window, 'confirm').mockReturnValue(true);
    const onConfirm = jest.fn();

    webConfirmDestructive({ title: 'Delete playlist', message: 'Are you sure?', confirmLabel: 'Delete', onConfirm });

    expect(window.confirm).toHaveBeenCalledWith('Delete playlist\n\nAre you sure?');
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('never runs onConfirm when window.confirm is declined', () => {
    const { confirmDestructive: webConfirmDestructive } = require('../confirmDestructive') as {
      confirmDestructive: typeof confirmDestructive;
    };
    jest.spyOn(window, 'confirm').mockReturnValue(false);
    const onConfirm = jest.fn();

    webConfirmDestructive({ title: 'Delete playlist', message: 'Are you sure?', confirmLabel: 'Delete', onConfirm });

    expect(window.confirm).toHaveBeenCalledTimes(1);
    expect(onConfirm).not.toHaveBeenCalled();
  });
});
