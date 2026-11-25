import { recordDialogs, type RecordedDialogs } from '@testing/recordDialogs';

import { confirmDestructive, showAlert } from '@shared/ui';

let dialogs: RecordedDialogs;

beforeEach(() => {
  dialogs = recordDialogs();
});

afterEach(() => {
  dialogs.restore();
});

describe('the dialogs the app installs', () => {
  it('shows an alert through the installed dialogs, with its title and message', () => {
    showAlert('Download failed', 'Not enough storage');

    expect(dialogs.alerts).toEqual([['Download failed', 'Not enough storage']]);
  });

  it('asks a confirmation through the installed dialogs and runs onConfirm only when they do', () => {
    const onConfirm = jest.fn();
    const options = { title: 'Delete', message: 'Sure?', confirmLabel: 'Delete', onConfirm };

    confirmDestructive(options);

    expect(dialogs.confirms).toEqual([options]);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('goes back to the platform dialogs once the fake is restored', () => {
    dialogs.restore();

    showAlert('Back', 'on the platform');

    expect(dialogs.alerts).toEqual([]);
  });
});
