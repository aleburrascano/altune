import {
  ContractError,
  isApiErrorCode,
  type ApiErrorCode,
  type ApiErrorExtras,
} from '@shared/failure';

export interface ApiErrorBody {
  detail?: string;
  code?: ApiErrorCode;
  rawCode?: string;
  type?: string;
  title?: string;
  status?: number;
  instance?: string;
}

export function asRecord(value: unknown, at: string): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new ContractError(at, 'expected an object');
  }
  return value as Record<string, unknown>;
}

export function asArray(value: unknown, at: string): unknown[] {
  if (!Array.isArray(value)) throw new ContractError(at, 'expected an array');
  return value;
}

export function asString(value: unknown, at: string): string {
  if (typeof value !== 'string') throw new ContractError(at, 'expected a string');
  return value;
}

const HTTPS_URL = /^https:\/\/[^\s\u0000-\u001f\u007f]+$/i;

export function asHttpsUrl(value: unknown, at: string): string {
  const text = asString(value, at);
  if (!HTTPS_URL.test(text)) throw new ContractError(at, 'expected an https url');
  return text;
}

export function asNumber(value: unknown, at: string): number {
  if (typeof value !== 'number') throw new ContractError(at, 'expected a number');
  return value;
}

export function asBoolean(value: unknown, at: string): boolean {
  if (typeof value !== 'boolean') throw new ContractError(at, 'expected a boolean');
  return value;
}

export function asCount(value: unknown, at: string): number {
  const count = asNumber(value, at);
  if (!Number.isInteger(count) || count < 0) {
    throw new ContractError(at, 'expected a non-negative integer');
  }
  return count;
}

export function parseArray<T>(
  value: unknown,
  at: string,
  parseItem: (item: unknown, at: string) => T,
): T[] {
  return asArray(value, at).map((item, i) => parseItem(item, `${at}[${i}]`));
}

export function parseListEnvelope<T>(
  r: Record<string, unknown>,
  at: string,
  parseItem: (item: unknown, at: string) => T,
): { items: T[]; total: number } {
  return {
    items: parseArray(r.items, `${at}.items`, parseItem),
    total: asNumber(r.total, `${at}.total`),
  };
}

export function nullableString(value: unknown, at: string): string | null {
  return value == null ? null : asString(value, at);
}

export function nullableBoolean(value: unknown, at: string): boolean | null {
  return value == null ? null : asBoolean(value, at);
}

export function nullableNumber(value: unknown, at: string): number | null {
  return value == null ? null : asNumber(value, at);
}

function optionalString(record: Record<string, unknown>, key: string): string | undefined {
  const value = record[key];
  return typeof value === 'string' ? value : undefined;
}

function optionalNumber(record: Record<string, unknown>, key: string): number | undefined {
  const value = record[key];
  return typeof value === 'number' ? value : undefined;
}

export function errorExtrasOf(
  body: ApiErrorBody,
  retryAfterMs: number | undefined,
): ApiErrorExtras {
  const extras = { rawCode: body.rawCode, detail: body.detail, retryAfterMs };
  return Object.fromEntries(Object.entries(extras).filter(([, field]) => field !== undefined));
}

export function parseErrorBody(value: unknown): ApiErrorBody {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return {};
  const record = value as Record<string, unknown>;
  const rawCode = optionalString(record, 'code');
  const known = isApiErrorCode(rawCode) ? rawCode : undefined;
  const fields = {
    detail: optionalString(record, 'detail'),
    type: optionalString(record, 'type'),
    title: optionalString(record, 'title'),
    status: optionalNumber(record, 'status'),
    instance: optionalString(record, 'instance'),
    code: known,
    rawCode: known === undefined ? rawCode : undefined,
  };
  return Object.fromEntries(Object.entries(fields).filter(([, field]) => field !== undefined));
}

const MS_PER_SECOND = 1000;
const DELTA_SECONDS_PATTERN = /^\d+$/;

export function retryAfterMsOf(header: string | null): number | undefined {
  if (header === null || !DELTA_SECONDS_PATTERN.test(header.trim())) return undefined;
  return Number(header.trim()) * MS_PER_SECOND;
}

export function member<T extends string>(value: unknown, allowed: readonly T[], at: string): T {
  const text = asString(value, at);
  if (!allowed.includes(text as T)) {
    throw new ContractError(at, `not one of ${allowed.join(', ')}`);
  }
  return text as T;
}
