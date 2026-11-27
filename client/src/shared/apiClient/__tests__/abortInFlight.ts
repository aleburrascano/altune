import { waitFor } from '@testing-library/react-native';

const { __http } = require('@testing/doubles/fetch.js');

export async function expectAbortsInFlight(
  spec: string,
  call: (signal: AbortSignal) => Promise<unknown>,
): Promise<void> {
  __http.hang(spec);
  const controller = new AbortController();

  const pending = call(controller.signal);
  await waitFor(() => {
    expect(__http.requests.length).toBeGreaterThan(0);
  });
  controller.abort();

  await expect(pending).rejects.toThrow();
  expect(__http.last().signal.aborted).toBe(true);
}
