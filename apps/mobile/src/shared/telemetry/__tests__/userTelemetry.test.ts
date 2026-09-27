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

describe('user telemetry: trim boundaries (message / error trimmed to 300 chars)', () => {
  it('sends a 300-character failure message unchanged', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });
    const exactly300 = 'm'.repeat(300);

    recordFailureShown({ surface: 'banner.track_status', message: exactly300 });
    await settle();

    expect(sentBody().payload.message).toBe(exactly300);
  });

  it('cuts a 301-character failure message to its first 300 characters', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    recordFailureShown({ surface: 'banner.track_status', message: `${'a'.repeat(300)}Z` });
    await settle();

    expect(sentBody().payload.message).toBe('a'.repeat(300));
  });

  it('sends a 299-character error unchanged', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });
    const just299 = 'e'.repeat(299);

    recordUserAction({ action: 'detail.save', outcome: 'failed', error: just299 });
    await settle();

    expect(sentBody().payload.error).toBe(just299);
  });

  it('keeps the start of a long error, not its end', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    recordUserAction({
      action: 'detail.save',
      outcome: 'failed',
      error: `HEAD${'-'.repeat(296)}TAIL${'-'.repeat(100)}`,
    });
    await settle();

    expect(sentBody().payload.error).toBe(`HEAD${'-'.repeat(296)}`);
  });

  it('sends an empty failure message as an empty string', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    recordFailureShown({ surface: 'banner.track_status', message: '' });
    await settle();

    expect(sentBody().payload.message).toBe('');
  });

  it('keeps a 300-character accented message (single code units) whole', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });
    const accented = 'é'.repeat(300);

    recordFailureShown({ surface: 'banner.track_status', message: accented });
    await settle();

    expect(sentBody().payload.message).toBe(accented);
  });

  it('keeps an emoji wholly inside the limit intact', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });
    const message = `${'a'.repeat(290)}😀`;

    recordFailureShown({ surface: 'banner.track_status', message });
    await settle();

    expect(sentBody().payload.message).toBe(message);
  });

  it('never cuts an emoji in half when a long message is trimmed', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    recordFailureShown({ surface: 'banner.track_status', message: 'a'.repeat(299) + '😀😀😀' });
    await settle();

    const sent = sentBody().payload.message as string;
    expect(sent.startsWith('a'.repeat(299))).toBe(true);
    expect(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/.test(sent)).toBe(false);
  });

  it('never cuts an emoji in half when a long error is trimmed', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    recordUserAction({ action: 'detail.save', outcome: 'failed', error: 'a'.repeat(299) + '🎵🎵🎵' });
    await settle();

    const sent = sentBody().payload.error as string;
    expect(sent.startsWith('a'.repeat(299))).toBe(true);
    expect(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/.test(sent)).toBe(false);
  });
});

describe('user telemetry: envelope and privacy (small flat payloads, session_id added by recordEvent)', () => {
  it('POSTs a user_action carrying only the fields the caller gave plus session_id', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    recordUserAction({ action: 'library.retry', outcome: 'succeeded' });
    await settle();

    const request = __http.last();
    expect(request.method).toBe('POST');
    expect(request.path).toBe('/v1/discovery/events');
    const body = sentBody();
    expect(Object.keys(body.payload).sort()).toEqual(['action', 'outcome', 'session_id']);
    expect(typeof body.payload.session_id).toBe('string');
  });

  it('POSTs a failure_shown carrying only surface, message and session_id when no track is named', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    recordFailureShown({ surface: 'alert.delete_track', message: 'could not delete' });
    await settle();

    const body = sentBody();
    expect(body.type).toBe('failure_shown');
    expect(Object.keys(body.payload).sort()).toEqual(['message', 'session_id', 'surface']);
  });

  it('sends a failed user_action with every optional field populated and flat', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });

    recordUserAction({
      action: 'detail.save',
      outcome: 'failed',
      track_id: 't-9',
      status: 503,
      correlation_id: 'corr-9',
      error: 'service unavailable',
    });
    await settle();

    const body = sentBody();
    expect(body.type).toBe('user_action');
    expect(body.payload).toEqual({
      action: 'detail.save',
      outcome: 'failed',
      track_id: 't-9',
      status: 503,
      correlation_id: 'corr-9',
      error: 'service unavailable',
      session_id: expect.any(String),
    });
  });
});

describe('user telemetry: errors are swallowed (fire-and-forget, never throws)', () => {
  function collectUnhandled(): { reasons: unknown[]; stop: () => void } {
    const reasons: unknown[] = [];
    const onUnhandled = (reason: unknown): void => {
      reasons.push(reason);
    };
    process.on('unhandledRejection', onUnhandled);
    return { reasons, stop: () => process.off('unhandledRejection', onUnhandled) };
  }

  async function flushMacrotasks(): Promise<void> {
    await settle();
    await new Promise((resolve) => setImmediate(resolve));
    await new Promise((resolve) => setImmediate(resolve));
  }

  it('does not throw when reading the session throws synchronously', async () => {
    getSession.mockReset().mockImplementation(() => {
      throw new Error('session store exploded');
    });
    const unhandled = collectUnhandled();

    expect(() => recordUserAction({ action: 'library.retry', outcome: 'tapped' })).not.toThrow();
    expect(() => recordFailureShown({ surface: 'banner.track_status', message: 'boom' })).not.toThrow();
    await flushMacrotasks();

    unhandled.stop();
    expect(unhandled.reasons).toEqual([]);
  });

  it('leaves no unhandled rejection when the request fails at the transport', async () => {
    __http.fail('POST /v1/discovery/events', __http.transportError());
    const unhandled = collectUnhandled();

    recordUserAction({ action: 'library.retry', outcome: 'tapped' });
    recordFailureShown({ surface: 'banner.track_status', message: 'boom' });
    await flushMacrotasks();

    unhandled.stop();
    expect(unhandled.reasons).toEqual([]);
  });

  it('leaves no unhandled rejection when the server answers 500', async () => {
    __http.reply('POST /v1/discovery/events', { status: 500 });
    const unhandled = collectUnhandled();

    recordUserAction({ action: 'detail.save', outcome: 'failed', status: 500 });
    recordFailureShown({ surface: 'alert.delete_track', message: 'boom' });
    await flushMacrotasks();

    unhandled.stop();
    expect(unhandled.reasons).toEqual([]);
    expect(__http.requests.length).toBe(2);
  });

  it('leaves no unhandled rejection when the telemetryFlush kill switch gates the send', async () => {
    applyKillSwitches({ telemetry_enabled: false });
    const unhandled = collectUnhandled();

    recordUserAction({ action: 'library.retry', outcome: 'tapped' });
    recordFailureShown({ surface: 'banner.track_status', message: 'boom' });
    await flushMacrotasks();

    unhandled.stop();
    expect(unhandled.reasons).toEqual([]);
    expect(__http.requests.length).toBe(0);
  });
});

describe('user telemetry: telemetryFlush kill switch', () => {
  it('sends nothing for a failed action with a long error while the switch is off', async () => {
    applyKillSwitches({ telemetry_enabled: false });

    recordUserAction({ action: 'detail.save', outcome: 'failed', status: 500, error: 'x'.repeat(1000) });
    await settle();
    await new Promise((resolve) => setImmediate(resolve));

    expect(__http.requests.length).toBe(0);
  });

  it('starts sending again once the switch is turned back on', async () => {
    __http.reply('POST /v1/discovery/events', { status: 202 });
    applyKillSwitches({ telemetry_enabled: false });
    recordFailureShown({ surface: 'banner.track_status', message: 'while off' });
    await settle();

    applyKillSwitches({ telemetry_enabled: true });
    recordFailureShown({ surface: 'banner.track_status', message: 'while on' });
    await settle();

    expect(__http.requests.length).toBe(1);
    expect(sentBody().payload.message).toBe('while on');
  });
});
