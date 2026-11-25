import type { AlertButton } from 'react-native';

import { createNativeDialogs } from '@shared/ui';

type ShownAlert = { title: string; message: string; buttons: AlertButton[] | undefined };

interface RecordingAlert {
  shown: ShownAlert[];
  alert: (...args: never[]) => void;
}

function nativeAlertRecording(): RecordingAlert {
  const shown: ShownAlert[] = [];
  return {
    shown,
    alert: ((title: string, message: string, buttons?: AlertButton[]) => {
      shown.push({ title, message, buttons });
    }) as (...args: never[]) => void,
  };
}

function dialogsOverARecordingAlert() {
  const native = nativeAlertRecording();
  return { dialogs: createNativeDialogs(native), shown: native.shown };
}

describe('native dialogs', () => {
  it('alerts with the title and message and no buttons', () => {
    const { dialogs, shown } = dialogsOverARecordingAlert();

    dialogs.alert('Download failed', 'Not enough storage');

    expect(shown).toEqual([
      { title: 'Download failed', message: 'Not enough storage', buttons: undefined },
    ]);
  });

  it('confirms with exactly a cancel button and a destructive button labelled as asked', () => {
    const { dialogs, shown } = dialogsOverARecordingAlert();

    dialogs.confirmDestructive({
      title: 'Delete playlist',
      message: 'Are you sure?',
      confirmLabel: 'Delete',
      onConfirm: jest.fn(),
    });

    expect(shown).toHaveLength(1);
    expect(shown[0]).toMatchObject({ title: 'Delete playlist', message: 'Are you sure?' });
    expect(shown[0]?.buttons).toEqual([
      { text: 'Cancel', style: 'cancel' },
      { text: 'Delete', style: 'destructive', onPress: expect.any(Function) },
    ]);
  });

  it('runs onConfirm once when the destructive button is pressed, and never on cancel', () => {
    const { dialogs, shown } = dialogsOverARecordingAlert();
    const onConfirm = jest.fn();
    dialogs.confirmDestructive({
      title: 'Remove track',
      message: 'Remove from playlist?',
      confirmLabel: 'Remove',
      onConfirm,
    });
    const buttons = shown[0]!.buttons!;

    buttons.find((button) => button.style === 'cancel')?.onPress?.();
    expect(onConfirm).not.toHaveBeenCalled();

    buttons.find((button) => button.style === 'destructive')?.onPress?.();
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });
});
