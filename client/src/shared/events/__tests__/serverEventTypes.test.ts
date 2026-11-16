import { SERVER_EVENT_TYPES, isServerEventType } from '@shared/events';

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
