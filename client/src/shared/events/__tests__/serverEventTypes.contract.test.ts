import { QueryClient } from '@tanstack/react-query';

import { SERVER_EVENT_TYPES, applyServerEvent, isServerEvent, parseChunk } from '@shared/events';
import { recordingCache } from '@shared/events/__tests__/recordingCache';
import { wireFixture } from '@testing/wireFixtures';

function publishedTypes(): string[] {
  return wireFixture('events_types') as string[];
}

function frameOf(type: string): string {
  return `id: 1\nevent: ${type}\ndata: ${JSON.stringify(wireFixture(`events_${type}`))}\n\n`;
}

describe('the event types go-api publishes, from the events_types wire fixture', () => {
  it('are exactly SERVER_EVENT_TYPES, with no name unpublished and none unhandled', () => {
    expect(publishedTypes().length).toBeGreaterThan(0);
    expect([...SERVER_EVENT_TYPES].sort()).toEqual([...publishedTypes()].sort());
  });

  it.each(publishedTypes())('%s: the wire frame parses and the handler accepts it', (type) => {
    const parsed = parseChunk('', frameOf(type), '');
    const block = parsed.blocks[0];

    expect(parsed.blocks).toHaveLength(1);
    expect(block !== undefined && isServerEvent(block)).toBe(true);
    if (block === undefined || !isServerEvent(block)) return;
    expect(block.type).toBe(type);

    const client = new QueryClient();
    expect(applyServerEvent(recordingCache(client), block)).toBe(true);
    client.clear();
  });
});
