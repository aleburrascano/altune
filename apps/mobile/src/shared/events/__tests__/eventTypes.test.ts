import {
  MAX_TRACKED_UNHANDLED_TYPES,
  SERVER_EVENT_TYPES,
  isServerEventType,
  recordUnhandledEvent,
  unhandledEventTypes,
  _resetUnhandledEventsForTest,
} from '../eventTypes';

describe('isServerEventType', () => {
  it.each(SERVER_EVENT_TYPES)('accepts %s as a known server event type', (type) => {
    expect(isServerEventType(type)).toBe(true);
  });

  it.each(['track_created', 'unknown', '', 'RESYNC', 'resync '])(
    'rejects %j as an unknown server event type',
    (type) => {
      expect(isServerEventType(type)).toBe(false);
    },
  );
});

describe('unhandled event tracking', () => {
  let warn: jest.SpyInstance;

  beforeEach(() => {
    _resetUnhandledEventsForTest();
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
  });

  it('starts empty right after a reset', () => {
    expect(unhandledEventTypes()).toEqual([]);
  });

  it('warns with the captured type on every occurrence, not only the first', () => {
    recordUnhandledEvent('mystery_event');
    recordUnhandledEvent('mystery_event');

    expect(warn).toHaveBeenCalledTimes(2);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('unrecognized event type'), {
      type: 'mystery_event',
    });
  });

  it('stops tracking further distinct types once the cap is reached', () => {
    for (let seen = 0; seen < MAX_TRACKED_UNHANDLED_TYPES; seen += 1) {
      recordUnhandledEvent(`future_event_${seen}`);
    }

    recordUnhandledEvent('one_type_too_many');

    expect(unhandledEventTypes()).toHaveLength(MAX_TRACKED_UNHANDLED_TYPES);
    expect(unhandledEventTypes()).not.toContain('one_type_too_many');
  });

  it('still warns about a type the cap refused to track', () => {
    for (let seen = 0; seen < MAX_TRACKED_UNHANDLED_TYPES; seen += 1) {
      recordUnhandledEvent(`future_event_${seen}`);
    }
    warn.mockClear();

    recordUnhandledEvent('one_type_too_many');

    expect(warn).toHaveBeenCalledWith(expect.any(String), { type: 'one_type_too_many' });
  });

  it('records a recurring unknown type only once', () => {
    recordUnhandledEvent('mystery_event');
    recordUnhandledEvent('mystery_event');
    recordUnhandledEvent('mystery_event');

    expect(unhandledEventTypes()).toEqual(['mystery_event']);
  });

  it('accumulates distinct unknown types across separate calls', () => {
    recordUnhandledEvent('a_future_event');
    recordUnhandledEvent('another_future_event');

    expect([...unhandledEventTypes()].sort()).toEqual([
      'a_future_event',
      'another_future_event',
    ]);
  });

  it('forgets prior recordings once reset', () => {
    recordUnhandledEvent('a_future_event');
    _resetUnhandledEventsForTest();

    expect(unhandledEventTypes()).toEqual([]);
  });
});
