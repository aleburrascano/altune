import { submitReport } from '../feedback';
import type { SubmitReportInput } from '../feedback';
import type { ApiError } from '../index';
import { NetworkError } from '../index';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const getSession = supabase.auth.getSession as jest.Mock;

function withSession(accessToken = 'tok') {
  getSession.mockResolvedValue({
    data: { session: { access_token: accessToken } },
    error: null,
  });
}

beforeEach(() => {
  getSession.mockReset();
  withSession();
});

const input: SubmitReportInput = {
  kind: 'bug',
  message: 'Playback stalls after skipping tracks quickly',
  app_version: '1.4.0',
  platform: 'ios',
  os_version: '17.5',
  screen: 'NowPlaying',
};

describe('submitReport', () => {
  it('POSTs /v1/feedback/reports with a JSON Content-Type and the exact serialized input', async () => {
    __http.reply('POST /v1/feedback/reports', {
      status: 201,
      json: { issue_number: 42, issue_url: 'https://github.com/x/y/issues/42' },
    });

    await submitReport(input);

    const request = __http.last();
    expect(request.method).toBe('POST');
    expect(request.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(request.body)).toEqual(input);
  });

  it('resolves with the issue_number and issue_url the tester is shown as confirmation', async () => {
    __http.reply('POST /v1/feedback/reports', {
      status: 201,
      json: { issue_number: 42, issue_url: 'https://github.com/x/y/issues/42' },
    });

    await expect(submitReport(input)).resolves.toEqual({
      issue_number: 42,
      issue_url: 'https://github.com/x/y/issues/42',
    });
  });

  it('rejects with ApiError(400) when the server rejects the message', async () => {
    __http.reply('POST /v1/feedback/reports', {
      status: 400,
      json: { message: 'message too short' },
    });

    await expect(submitReport(input)).rejects.toMatchObject({ status: 400 });
  });

  it('rejects with ApiError(404) when this deploy has no issue tracker configured', async () => {
    __http.reply('POST /v1/feedback/reports', { status: 404, json: {} });

    await expect(submitReport(input)).rejects.toMatchObject({ status: 404 });
  });

  it('distinguishes a rejected message (400) from a missing tracker (404) by status', async () => {
    __http.replyOnce('POST /v1/feedback/reports', { status: 400, json: {} });
    const rejected = (await submitReport(input).catch((e: unknown) => e)) as ApiError;

    __http.replyOnce('POST /v1/feedback/reports', { status: 404, json: {} });
    const missingTracker = (await submitReport(input).catch((e: unknown) => e)) as ApiError;

    expect(rejected.status).toBe(400);
    expect(missingTracker.status).toBe(404);
  });

  it('rejects with NetworkError on a dropped connection', async () => {
    __http.fail('POST /v1/feedback/reports');

    await expect(submitReport(input)).rejects.toBeInstanceOf(NetworkError);
  });

  it('rejects with NetworkError on a truncated response body', async () => {
    __http.reply('POST /v1/feedback/reports', { status: 200, malformed: true });

    await expect(submitReport(input)).rejects.toBeInstanceOf(NetworkError);
  });

  it('never leaks the access token into the request URL or a thrown error message', async () => {
    withSession('leak-check-token');
    __http.reply('POST /v1/feedback/reports', { status: 500, json: {} });

    const thrown = (await submitReport(input).catch((e: unknown) => e)) as Error;

    expect(__http.last().url).not.toContain('leak-check-token');
    expect(thrown.message).not.toContain('leak-check-token');
  });
});
