import { ContractError } from '@shared/errors';
import type { ApiErrorBody } from './types';

// Shared primitive wire decoders. Each domain's response parsers live beside the
// domain's endpoints (tracks.ts, playlists.ts, library.ts, discovery.ts) and build
// on these narrowers.

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

export function nullableNumber(value: unknown, at: string): number | null {
  return value == null ? null : asNumber(value, at);
}

function optionalString(record: Record<string, unknown>, key: string): string | undefined {
  const value = record[key];
  return typeof value === 'string' ? value : undefined;
}

export function parseErrorBody(value: unknown): ApiErrorBody {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return {};
  const record = value as Record<string, unknown>;
  const code = optionalString(record, 'code');
  const detail = optionalString(record, 'detail');
  return {
    ...(code !== undefined ? { code } : {}),
    ...(detail !== undefined ? { detail } : {}),
  };
}

export function member<T extends string>(value: unknown, allowed: readonly T[], at: string): T {
  const text = asString(value, at);
  if (!allowed.includes(text as T)) {
    throw new ContractError(at, `not one of ${allowed.join(', ')}`);
  }
  return text as T;
}
