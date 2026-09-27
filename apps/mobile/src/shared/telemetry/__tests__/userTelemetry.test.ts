import { supabase } from '@shared/auth/supabaseClient';
import { applyKillSwitches } from '@shared/killSwitch/killSwitch';

import { recordFailureShown, recordUserAction } from '../userTelemetry';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const getSession = supabase.auth.getSession as jest.Mock;

function sentBody(): { type: string; payload: Record<string, unknown> } {
  return JSON.parse(__http.last().body);
}

async function settle(): Promise<void> {
  for (let i = 0; i < 10; i += 1) await Promise.resolve();
}

beforeEach(() => {
  getSession.mockReset().mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  applyKillSwitches({ telemetry_enabled: true });
});

describe('recordUserAction()', () => {
  it('POSTs a user_action event carrying the action, outcome and track id', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    recordUserAction({ action: 'library.retry', outcome: 'tapped', track_id: 't-1' });
    await settle();

    const body = sentBody();
    expect(body.type).toBe('user_action');
    expect(body.payload).toMatchObject({
      action: 'library.retry',
      outcome: 'tapped',
      track_id: 't-1',
    });
  });

  it('carries the status and correlation id on a failed outcome', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    recordUserAction({
      action: 'detail.save',
      outcome: 'failed',
      status: 500,
      correlation_id: 'corr-1',
    });
    await settle();

    expect(sentBody().payload).toMatchObject({ status: 500, correlation_id: 'corr-1' });
  });

  it('trims an error message longer than 300 characters', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    recordUserAction({ action: 'detail.save', outcome: 'failed', error: 'x'.repeat(400) });
    await settle();

    expect((sentBody().payload.error as string).length).toBe(300);
  });

  it('never throws when the request rejects', async () => {
    __http.fail('POST /v1/discovery/events', __http.transportError());

    expect(() => recordUserAction({ action: 'library.retry', outcome: 'tapped' })).not.toThrow();
    await settle();
  });

  it('sends nothing while the telemetryFlush kill switch is off', async () => {
    applyKillSwitches({ telemetry_enabled: false });

    recordUserAction({ action: 'library.retry', outcome: 'tapped' });
    await settle();

    expect(__http.requests.length).toBe(0);
  });
});

describe('recordFailureShown()', () => {
  it('POSTs a failure_shown event carrying the surface, message and track id', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    recordFailureShown({ surface: 'alert.delete_track', message: 'could not delete', track_id: 't-2' });
    await settle();

    const body = sentBody();
    expect(body.type).toBe('failure_shown');
    expect(body.payload).toMatchObject({
      surface: 'alert.delete_track',
      message: 'could not delete',
      track_id: 't-2',
    });
  });

  it('trims a message longer than 300 characters', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    recordFailureShown({ surface: 'banner.track_status', message: 'y'.repeat(400) });
    await settle();

    expect((sentBody().payload.message as string).length).toBe(300);
  });

  it('never throws when the request rejects', async () => {
    __http.fail('POST /v1/discovery/events', __http.transportError());

    expect(() =>
      recordFailureShown({ surface: 'banner.track_status', message: 'boom' }),
    ).not.toThrow();
    await settle();
  });

  it('sends nothing while the telemetryFlush kill switch is off', async () => {
    applyKillSwitches({ telemetry_enabled: false });

    recordFailureShown({ surface: 'banner.track_status', message: 'boom' });
    await settle();

    expect(__http.requests.length).toBe(0);
  });
});
