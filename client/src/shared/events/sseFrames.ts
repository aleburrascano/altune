import type { JsonObject } from '@shared/files/json';

export interface ServerEvent {
  id: string;
  type: string;
  data: JsonObject;
}

class MalformedSSEEventError extends Error {
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

const CRLF_PATTERN = /\r\n/g;
const BARE_CR_PATTERN = /\r/g;

function withUnixLineEndings(text: string): string {
  return text.replace(CRLF_PATTERN, '\n').replace(BARE_CR_PATTERN, '\n');
}

interface BlockFields {
  id: string;
  type: string;
  dataLines: string[];
}

function applyField(fields: BlockFields, field: WireField): void {
  if (field.name === 'id') fields.id = field.value;
  else if (field.name === 'event') fields.type = field.value;
  else if (field.name === 'data') fields.dataLines.push(field.value);
}

function collectFields(block: string): BlockFields {
  const fields: BlockFields = { id: '', type: 'message', dataLines: [] };
  for (const line of block.split('\n')) {
    const field = fieldOf(line);
    if (field !== null) applyField(fields, field);
  }
  return fields;
}

function isJsonObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function parseJson(payload: string): unknown {
  try {
    return JSON.parse(payload) as unknown;
  } catch {
    return undefined;
  }
}

function parsePayload(id: string, type: string, payload: string): ParsedBlock {
  const parsed = parseJson(payload);
  if (isJsonObject(parsed)) return { id, type, data: parsed };
  return new MalformedSSEEventError(id, type, payload.length);
}

function parseBlock(block: string): ParsedBlock | null {
  const { id, type, dataLines } = collectFields(block);
  if (dataLines.length === 0) return null;
  return parsePayload(id, type, dataLines.join('\n'));
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
