import { enqueueCritical, flushOutbox, recordEvent } from '@shared/telemetry';
import type { DiscoveryEventType } from '@shared/telemetry';
import { EVENT_ROUTES } from '@testing/eventRoutes';
import { flushPromises } from '@testing/flushPromises';
import { signInTestUser } from '@testing/signInTestUser';
import { wireFixture } from '@testing/wireFixtures';

const { __http } = require('@testing/doubles/fetch.js');

const DISCOVERY_PATH = '/v1/discovery/events';
const TELEMETRY_PATH = '/v1/telemetry/events';
const HTTP_ACCEPTED = 202;
const UUID_PATTERN = /^[0-9a-f-]{36}$/;

const SEARCH_TYPES: DiscoveryEventType[] = [
  'results_shown',
  'result_clicked',
  'play',
  'skip',
  'completed',
  'library_add',
  'wrong_album',
  'search_failed',
  'search_degraded',
];

const OPERATIONAL_TYPES = wireFixture('telemetry_event_types') as DiscoveryEventType[];

beforeEach(async () => {
  await signInTestUser();
  __http.reply(EVENT_ROUTES, { status: HTTP_ACCEPTED });
});

describe('which route an event type goes to', () => {
  it.each(SEARCH_TYPES)('sends the search event %s to the discovery route', async (type) => {
    await recordEvent({ type });

    expect(__http.last().path).toBe(DISCOVERY_PATH);
  });

  it.each(OPERATIONAL_TYPES)(
    'sends the operational event %s to the telemetry route',
    async (type) => {
      await recordEvent({ type });

      expect(__http.last().path).toBe(TELEMETRY_PATH);
    },
  );

  it('knows no type outside the two lists, so a new type must pick a route', () => {
    const submittable = wireFixture('discovery_event_types_client_submittable') as string[];

    expect([...SEARCH_TYPES, ...OPERATIONAL_TYPES].sort()).toEqual([...submittable].sort());
  });
});

describe('what an operational event carries to the telemetry route', () => {
  it('sends the correlation id as a top-level field, with message and track_id in the payload', async () => {
    await recordEvent({
      type: 'failure_shown',
      payload: {
        surface: 'detail',
        message: 'could not save',
        track_id: 't-1',
        correlation_id: 'c-1',
      },
    });

    const sent = JSON.parse(__http.last().body);
    expect(__http.last().path).toBe(TELEMETRY_PATH);
    expect(sent.correlation_id).toBe('c-1');
    expect(sent.payload).toEqual({
      surface: 'detail',
      message: 'could not save',
      track_id: 't-1',
      session_id: expect.any(String),
    });
  });

  it('sends no search_id and no discovery request for an operational event', async () => {
    await recordEvent({ type: 'client_error', event_id: 'e-1', payload: { message: 'boom' } });

    expect(__http.countFor('POST /v1/discovery/events')).toBe(0);
    expect(JSON.parse(__http.last().body)).not.toHaveProperty('search_id');
  });
});

describe('the events that need an id carry one on the telemetry route', () => {
  it.each(['client_error', 'acquisition_ui'] as const)('stamps event_id on %s', async (type) => {
    await enqueueCritical({ type, payload: {} });
    await flushOutbox();
    await flushPromises();

    const sent = JSON.parse(__http.last().body) as { event_id?: string };
    expect(__http.last().path).toBe(TELEMETRY_PATH);
    expect(sent.event_id).toMatch(UUID_PATTERN);
  });
});
