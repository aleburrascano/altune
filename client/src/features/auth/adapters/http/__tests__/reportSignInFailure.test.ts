import { reportSignInFailure } from '@features/auth/adapters/http/reportSignInFailure';
import { thrownErrorDetail } from '@features/auth/domain/authError';

describe('reportSignInFailure', () => {
  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('logs a warning with the reason and error detail when the report is not delivered', async () => {
    const error = new Error('offline');
    jest.spyOn(global, 'fetch').mockRejectedValue(error);
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);

    reportSignInFailure('network');
    await new Promise((resolve) => setImmediate(resolve));

    expect(warn).toHaveBeenCalledWith('[auth] sign-in failure report not delivered', {
      reason: 'network',
      ...thrownErrorDetail(error),
    });
  });
});
