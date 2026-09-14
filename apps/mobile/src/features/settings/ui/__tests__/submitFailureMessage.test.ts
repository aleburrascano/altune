import { ApiError } from '@shared/api-client';
import { submitFailureMessage } from '../submitFailureMessage';

const REJECTED = 'That report was rejected — try describing it in a bit more detail.';
const UNREACHABLE =
  'Could not reach the server. Your report is saved — try again when you have signal.';

describe('submitFailureMessage(): maps a submit error to user-facing copy', () => {
  it.each([
    ['ApiError 400', new ApiError(400, 'bad request'), REJECTED],
    ['ApiError 500', new ApiError(500, 'boom'), UNREACHABLE],
    ['ApiError 422', new ApiError(422, 'unprocessable'), UNREACHABLE],
    ['plain Error', new Error('offline'), UNREACHABLE],
    ['non-error value', 'nope', UNREACHABLE],
  ] as const)('%s', (_label, error, expected) => {
    expect(submitFailureMessage(error)).toBe(expected);
  });
});
