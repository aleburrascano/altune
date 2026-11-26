const { getConfig } = require('@testing-library/react-native/build/config');

const { __http } = require('../doubles/fetch.js');
const { supabase } = require('../../src/shared/auth');
const { signInTestUser } = require('../signInTestUser');
const { recordLogs } = require('../fakes/log/inMemoryLogger');

beforeEach(async () => {
  recordLogs();
  await signInTestUser();
});

describe('setup-after-env', () => {
  it('installs the app auth client, so code that reads the session has one', () => {
    expect(supabase.auth.storageKey).toBe('sb-fixture-auth-token');
  });

  it('raises the RNTL asyncUtilTimeout so waitFor tolerates a loaded machine', () => {
    expect(getConfig().asyncUtilTimeout).toBe(5000);
  });

  describe('outbox reset between tests', () => {
    const { enqueueCritical } = require('../../src/shared/telemetry');

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
    const { warn } = require('../../src/shared/log');
    const { recordLogs } = require('../fakes/log/inMemoryLogger');
    let earlier: { warnings: unknown[] };

    beforeAll(() => {
      earlier = recordLogs();
    });

    it('starts the next test on the console, not on a recorder an earlier one installed', () => {
      warn('[setup] this warning must not reach the earlier recorder');
      expect(earlier.warnings).toEqual([]);
    });
  });

  describe('app state reset between tests', () => {
    const { subscribeAppState } = require('../../src/shared/lifecycle');
    const { driveAppState } = require('../fakes/lifecycle/inMemoryAppStateSource');
    const heard: string[] = [];
    let earlier: { change(state: string): void };

    beforeAll(() => {
      earlier = driveAppState();
      subscribeAppState((state: string) => heard.push(state));
    });

    it('starts the next test on the native app state, not on a driver an earlier one installed', () => {
      earlier.change('background');
      expect(heard).toEqual([]);
    });
  });
});
