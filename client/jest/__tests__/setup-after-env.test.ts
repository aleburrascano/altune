const { getConfig } = require('@testing-library/react-native/build/config');

const { __http } = require('../doubles/fetch.js');
const { supabase } = require('../../src/shared/auth/supabaseClient');

beforeEach(() => {
  jest.spyOn(supabase.auth, 'getSession').mockResolvedValue({
    data: { session: { access_token: 'tok', user: { id: 'user-1' } } },
    error: null,
  });
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

describe('setup-after-env', () => {
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
});
