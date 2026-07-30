import {
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
  beforeEach(() => {
    _resetUnhandledEventsForTest();
  });

  it('starts empty right after a reset', () => {
    expect(unhandledEventTypes()).toEqual([]);
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
