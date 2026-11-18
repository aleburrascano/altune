const { getConfig } = require('@testing-library/react-native/build/config');

const { __http } = require('../doubles/fetch.js');
const { supabase } = require('../../src/shared/auth');

beforeEach(() => {
  jest.spyOn(supabase.auth, 'getSession').mockResolvedValue({
    data: { session: { access_token: 'tok', user: { id: 'user-1' } } },
    error: null,
  });
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

describe('setup-after-env', () => {
  it('installs the app auth client, so code that reads the session has one', () => {
    expect(supabase.auth.storageKey).toBe('sb-fixture-auth-token');
  });

  it('raises the RNTL asyncUtilTimeout so waitFor tolerates a loaded machine', () => {
    expect(getConfig().asyncUtilTimeout).toBe(5000);
  });

  describe('outbox reset between tests', () => {
    const { enqueueCritical } = require('../../src/shared/telemetry/outbox');

    beforeAll(async () => {
      __http.fail('POST /v1/discovery/events');
      await enqueueCritical({ type: 'user_action' });
    });

    it('starts the next test with no backoff suppressing the flush', async () => {
      __http.reply('POST /v1/discovery/events', { status: 204 });
      await enqueueCritical({ type: 'user_action' });
      expect(__http.countFor('POST /v1/discovery/events')).toBe(1);
    });
  });

  describe('logger reset between tests', () => {
    const { recordLogs, warn } = require('../../src/shared/log');
    let earlier: { warnings: unknown[] };

    beforeAll(() => {
      earlier = recordLogs();
    });

    it('starts the next test on the console, not on a recorder an earlier one installed', () => {
      warn('[setup] this warning must not reach the earlier recorder');
      expect(earlier.warnings).toEqual([]);
    });
  });
});
