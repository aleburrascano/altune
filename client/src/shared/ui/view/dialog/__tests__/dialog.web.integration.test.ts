import { confirmDestructive, showAlert } from '@shared/ui';

const PROMPTS_ASKED = 2;

const realAlert = window.alert;
const realConfirm = window.confirm;

let alerts: string[];
let questions: string[];
let answer: boolean;

beforeEach(() => {
  alerts = [];
  questions = [];
  answer = true;
  window.alert = (message?: string) => {
    alerts.push(message ?? '');
  };
  window.confirm = (message?: string) => {
    questions.push(message ?? '');
    return answer;
  };
});

afterEach(() => {
  window.alert = realAlert;
  window.confirm = realConfirm;
});

const DELETE_PLAYLIST = {
  title: 'Delete playlist',
  message: 'Are you sure?',
  confirmLabel: 'Delete',
};

describe('web dialogs', () => {
  it('alerts with the title and message in one browser alert', () => {
    showAlert('Download failed', 'Not enough storage');

    expect(alerts).toEqual(['Download failed\n\nNot enough storage']);
  });

  it('asks once, and runs onConfirm only when the user accepts', () => {
    const onConfirm = jest.fn();
    confirmDestructive({ ...DELETE_PLAYLIST, onConfirm });

    expect(questions).toEqual(['Delete playlist\n\nAre you sure?']);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('never runs onConfirm when the user declines', () => {
    answer = false;
    const onConfirm = jest.fn();

    confirmDestructive({ ...DELETE_PLAYLIST, onConfirm });

    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('runs onConfirm once per accepted prompt', () => {
    const onConfirm = jest.fn();
    confirmDestructive({ ...DELETE_PLAYLIST, onConfirm });
    answer = false;
    confirmDestructive({ ...DELETE_PLAYLIST, onConfirm });

    expect(questions).toHaveLength(PROMPTS_ASKED);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });
});
