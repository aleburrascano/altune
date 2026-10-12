import { Alert } from 'react-native';

import { signInTestUser } from '@testing/signInTestUser';

import { showFailureAlert } from '@shared/ui/failure/showFailure';
import { showFailureAlert as showFailureAlertFromUi } from '@shared/ui';

const { __http } = require('@testing/doubles/fetch.js');

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));

const SETTLE_TICKS = 20;
const MAX_RECORDED_MESSAGE = 200;
const OVERFLOW = 50;

type RecordedEvent = { type: string; payload: Record<string, unknown> };
type RecordedRequest = { body: string };

let alertSpy: jest.SpyInstance;

async function settle(): Promise<void> {
  for (let i = 0; i < SETTLE_TICKS; i += 1) await Promise.resolve();
}

function recordedEvents(): RecordedEvent[] {
  return __http.requests.map((request: RecordedRequest) => JSON.parse(request.body));
}

beforeEach(async () => {
  __http.reply('POST /v1/discovery/events', { status: 202 });
  await signInTestUser();
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(jest.fn());
});

afterEach(() => {
  alertSpy.mockRestore();
});

describe('showFailureAlert', () => {
  it('records one failure_shown then shows the alert once', async () => {
    showFailureAlert({ surface: 'library.retry', title: 'Oops', message: 'Nope', trackId: 't1' });
    expect(alertSpy.mock.calls).toEqual([['Oops', 'Nope']]);
    await settle();

    const events = recordedEvents();
    expect(events).toHaveLength(1);
    expect(events[0]?.type).toBe('failure_shown');
    expect(events[0]?.payload).toMatchObject({
      surface: 'library.retry',
      message: 'Nope',
      track_id: 't1',
    });
  });

  it('caps the recorded message at 200 chars but alerts the full text', async () => {
    const message = 'x'.repeat(MAX_RECORDED_MESSAGE + OVERFLOW);

    showFailureAlert({ surface: 's', title: 'T', message });
    await settle();

    expect((recordedEvents()[0]?.payload.message as string).length).toBe(MAX_RECORDED_MESSAGE);
    expect(alertSpy.mock.calls).toEqual([['T', message]]);
  });

  it('is exported from @shared/ui', () => {
    expect(showFailureAlertFromUi).toBe(showFailureAlert);
  });
});
