import { QueryClient } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/api-client';
import { submitReport } from '@shared/api-client/feedback';

import { makeWrapper } from '../../../../jest/makeWrapper';
import { useSubmitReport } from '../hooks/useSubmitReport';

jest.mock('@shared/api-client/feedback', () => ({
  ...jest.requireActual('@shared/api-client/feedback'),
  submitReport: jest.fn(),
}));

describe('settings mutations retry transient failures', () => {
  // #841: mutations default to zero retries, so a transient 502 on backfill or
  // clear-history failed outright. They must retry transient failures via isRetryable().

  function makeClient() {
    // No mutation defaults: each hook owns both the retry decision and its delay.
    return new QueryClient();
  }

  const badGateway = () => new ApiError(502, 'bad gateway');

  beforeEach(() => {
    jest.useFakeTimers();
    jest.mocked(submitReport).mockReset();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it('keeps report submission single-shot even on a transient 502', async () => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    jest.mocked(submitReport).mockRejectedValue(badGateway());

    const hook = renderHook(() => useSubmitReport(), { wrapper: makeWrapper(makeClient()) });
    act(() => {
      hook.result.current.mutate({} as never);
    });

    await waitFor(() => expect(hook.result.current.isError).toBe(true));
    expect(submitReport).toHaveBeenCalledTimes(1);
    hook.unmount();
  });
});
