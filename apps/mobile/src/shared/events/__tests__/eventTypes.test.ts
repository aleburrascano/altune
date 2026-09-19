import { SERVER_EVENT_TYPES, isServerEventType, recordUnhandledEvent } from '../eventTypes';

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

describe('unhandled event reporting', () => {
  let warn: jest.SpyInstance;

  beforeEach(() => {
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
  });

  it('warns with the captured type on every occurrence, not only the first', () => {
    recordUnhandledEvent('mystery_event');
    recordUnhandledEvent('mystery_event');

    expect(warn).toHaveBeenCalledTimes(2);
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('unrecognized event type'), {
      type: 'mystery_event',
    });
  });

  it('names each distinct type in its own warning', () => {
    recordUnhandledEvent('a_future_event');
    recordUnhandledEvent('another_future_event');

    expect(warn.mock.calls.map(([, detail]) => detail)).toEqual([
      { type: 'a_future_event' },
      { type: 'another_future_event' },
    ]);
  });
});
