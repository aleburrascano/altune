import { SERVER_EVENT_TYPES, isServerEvent, isServerEventType, parseChunk } from '@shared/events';
import { wireFixture } from '@testing/wireFixtures';

const TYPES_FIXTURE = 'events_types';

function publishedTypes(): string[] {
  return wireFixture(TYPES_FIXTURE) as string[];
}

describe('server events against the go-api wire fixtures', () => {
  it('knows exactly the event types go-api publishes', () => {
    expect([...SERVER_EVENT_TYPES].sort()).toEqual([...publishedTypes()].sort());
  });

  it.each(publishedTypes())('%s: the real SSE parser reads its fixture payload', (type) => {
    const payload = wireFixture(`events_${type}`);
    const frame = `id: 7\nevent: ${type}\ndata: ${JSON.stringify(payload)}\n\n`;

    const { blocks } = parseChunk('', frame, '');

    expect(blocks).toHaveLength(1);
    const block = blocks[0];
    if (block === undefined || !isServerEvent(block))
      throw new Error(`${type} did not parse as a server event`);
    expect(block.type).toBe(type);
    expect(isServerEventType(block.type)).toBe(true);
    expect(block.data).toEqual(payload);
  });
});
