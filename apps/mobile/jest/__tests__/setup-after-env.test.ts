const { getConfig } = require('@testing-library/react-native/build/config');

jest.mock('../../src/shared/telemetry/recordEvent', () => ({
  recordEvent: jest.fn(),
}));

beforeEach(() => {
  require('../../src/shared/telemetry/recordEvent').recordEvent.mockReset();
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

describe('setup-after-env', () => {
  it('raises the RNTL asyncUtilTimeout so waitFor tolerates a loaded machine', () => {
    expect(getConfig().asyncUtilTimeout).toBe(5000);
  });

  describe('outbox reset between tests', () => {
    const { enqueueCritical } = require('../../src/shared/telemetry/outbox');
    const { recordEvent } = require('../../src/shared/telemetry/recordEvent');

    beforeAll(async () => {
      recordEvent.mockRejectedValue(new Error('boom'));
      await enqueueCritical({ type: 'user_action' });
    });

    it('starts the next test with no backoff suppressing the flush', async () => {
      recordEvent.mockResolvedValue(undefined);
      await enqueueCritical({ type: 'user_action' });
      expect(recordEvent).toHaveBeenCalledTimes(1);
    });
  });
});
