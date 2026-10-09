export interface ServerEvent {
  id: string;
  type: string;
  data: Record<string, unknown>;
}

export class MalformedSSEEventError extends Error {
  readonly eventId: string;
  readonly eventType: string;
  readonly payloadLength: number;

  constructor(eventId: string, eventType: string, payloadLength: number) {
    super(
      `malformed SSE event payload (type=${eventType}, id=${eventId || '<none>'}, length=${payloadLength})`,
    );
    this.name = 'MalformedSSEEventError';
    this.eventId = eventId;
    this.eventType = eventType;
    this.payloadLength = payloadLength;
  }
}

type WireField = { name: string; value: string };

function fieldOf(line: string): WireField | null {
  if (line.startsWith(':')) return null;
  const colon = line.indexOf(':');
  if (colon === -1) return null;
  const name = line.substring(0, colon);
  const raw = line.substring(colon + 1);
  return { name, value: raw.startsWith(' ') ? raw.substring(1) : raw };
}

export type ParsedBlock = ServerEvent | MalformedSSEEventError;

interface ParsedChunk {
  blocks: ParsedBlock[];
  remainder: string;
  lastEventId: string;
}

export function isServerEvent(block: ParsedBlock): block is ServerEvent {
  return !(block instanceof MalformedSSEEventError);
}

function withUnixLineEndings(text: string): string {
  return text.replace(/\r\n/g, '\n').replace(/\r/g, '\n');
}

function parseBlock(block: string): ParsedBlock | null {
  let id = '';
  let type = 'message';
  const dataLines: string[] = [];

  for (const line of block.split('\n')) {
    const field = fieldOf(line);
    if (field === null) continue;
    if (field.name === 'id') id = field.value;
    else if (field.name === 'event') type = field.value;
    else if (field.name === 'data') dataLines.push(field.value);
  }

  if (dataLines.length === 0) return null;

  const payload = dataLines.join('\n');
  try {
    return { id, type, data: JSON.parse(payload) as Record<string, unknown> };
  } catch {
    return new MalformedSSEEventError(id, type, payload.length);
  }
}

function latestEventId(blocks: ParsedBlock[], fallback: string): string {
  let latest = fallback;
  for (const block of blocks) {
    if (isServerEvent(block) && block.id) latest = block.id;
  }
  return latest;
}

export function parseChunk(buffer: string, chunk: string, lastEventId: string): ParsedChunk {
  const rawBlocks = (buffer + withUnixLineEndings(chunk)).split('\n\n');
  const remainder = rawBlocks.pop() ?? '';
  const blocks: ParsedBlock[] = [];
  for (const raw of rawBlocks) {
    const block = parseBlock(raw);
    if (block !== null) blocks.push(block);
  }
  return { blocks, remainder, lastEventId: latestEventId(blocks, lastEventId) };
}
