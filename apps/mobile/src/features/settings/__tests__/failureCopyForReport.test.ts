import { ApiError, ContractError, NetworkError } from '@shared/api-client';
import { failureCopyForReport } from '../failureCopyForReport';

const REJECTED = 'That report was rejected — try describing it in a bit more detail.';
const SIGNED_OUT = 'You need to be signed in to send a report — sign in again and retry.';
const RATE_LIMITED = 'You have sent a lot of reports recently — try again in a little while.';
const NOT_ACCEPTED = 'The server could not accept this report (error 404). Try again later.';
const SERVER_DOWN = 'The server had a problem filing your report — try again in a few minutes.';
const UNREACHABLE = 'Could not reach the server — check your connection and try again.';
const UNKNOWN = 'Something went wrong sending your report — try again.';

describe('failureCopyForReport(): maps a submit error to status-appropriate copy', () => {
  it.each([
    ['ApiError 400', new ApiError(400, 'bad request'), REJECTED],
    ['ApiError 401', new ApiError(401, 'unauthorized'), SIGNED_OUT],
    ['ApiError 403', new ApiError(403, 'forbidden'), SIGNED_OUT],
    ['ApiError 429', new ApiError(429, 'slow down'), RATE_LIMITED],
    ['ApiError 404', new ApiError(404, 'not found'), NOT_ACCEPTED],
    [
      'ApiError 422',
      new ApiError(422, 'unprocessable'),
      'The server could not accept this report (error 422). Try again later.',
    ],
    ['ApiError 500', new ApiError(500, 'boom'), SERVER_DOWN],
    ['ApiError 503', new ApiError(503, 'unavailable'), SERVER_DOWN],
    ['NetworkError transport', new NetworkError('transport', 'offline'), UNREACHABLE],
    ['NetworkError timeout', new NetworkError('timeout', 'slow'), UNREACHABLE],
    ['ContractError', new ContractError('$', 'expected an object'), UNKNOWN],
    ['plain Error', new Error('offline'), UNKNOWN],
    ['non-error value', 'nope', UNKNOWN],
  ] as const)('%s', (_label, error, expected) => {
    expect(failureCopyForReport(error)).toBe(expected);
  });

  it('never claims the draft is saved, since nothing persists it', () => {
    const errors: unknown[] = [
      new ApiError(400, ''),
      new ApiError(401, ''),
      new ApiError(429, ''),
      new ApiError(404, ''),
      new ApiError(500, ''),
      new NetworkError('transport', ''),
      new Error(''),
    ];
    for (const error of errors) {
      expect(failureCopyForReport(error)).not.toMatch(/saved/i);
    }
  });
});
