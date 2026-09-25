import { ContractError, isRetryable } from '@shared/errors';
import {
  asArray,
  asBoolean,
  asNumber,
  asRecord,
  asString,
  member,
  nullableNumber,
  nullableString,
  parseErrorBody,
} from '../wireDecoders';

describe('primitive narrowers return the value or throw a ContractError', () => {
  it('asRecord accepts a plain object and rejects a number, null, and an array', () => {
    expect(asRecord({ a: 1 }, 'r')).toEqual({ a: 1 });
    expect(() => asRecord(42, 'r')).toThrow(ContractError);
    expect(() => asRecord(null, 'r')).toThrow(ContractError);
    expect(() => asRecord([], 'r')).toThrow(ContractError);
  });

  it('asArray accepts an array and rejects an object', () => {
    expect(asArray([1, 2], 'a')).toEqual([1, 2]);
    expect(() => asArray({}, 'a')).toThrow(ContractError);
  });

  it('asString accepts a string and rejects a number', () => {
    expect(asString('x', 's')).toBe('x');
    expect(() => asString(5, 's')).toThrow(ContractError);
  });

  it('asNumber accepts a number and rejects a string', () => {
    expect(asNumber(5, 'n')).toBe(5);
    expect(() => asNumber('x', 'n')).toThrow(ContractError);
  });

  it('asBoolean accepts a boolean and rejects a string', () => {
    expect(asBoolean(true, 'b')).toBe(true);
    expect(() => asBoolean('x', 'b')).toThrow(ContractError);
  });

  it('nullableString maps null and undefined to null, carries a string, and rejects a number', () => {
    expect(nullableString(null, 'ns')).toBeNull();
    expect(nullableString(undefined, 'ns')).toBeNull();
    expect(nullableString('x', 'ns')).toBe('x');
    expect(() => nullableString(5, 'ns')).toThrow(ContractError);
  });

  it('nullableNumber maps null and undefined to null, carries a number, and rejects a string', () => {
    expect(nullableNumber(null, 'nn')).toBeNull();
    expect(nullableNumber(undefined, 'nn')).toBeNull();
    expect(nullableNumber(7, 'nn')).toBe(7);
    expect(() => nullableNumber('x', 'nn')).toThrow(ContractError);
  });

  it('member accepts a declared value, rejects an undeclared one, and rejects a non-string', () => {
    expect(member('ready', ['pending', 'ready', 'failed'] as const, 'm')).toBe('ready');
    expect(() => member('nope', ['pending', 'ready', 'failed'] as const, 'm')).toThrow(
      ContractError,
    );
    expect(() => member(5, ['pending', 'ready', 'failed'] as const, 'm')).toThrow(ContractError);
  });

  it('classifies a ContractError as non-retryable (an off-contract body will not heal on retry)', () => {
    expect(isRetryable(new ContractError('x', 'y'))).toBe(false);
  });
});

describe('parseErrorBody — lenient extraction of the machine-readable error code', () => {
  it('pulls code and detail from a well-formed error body', () => {
    expect(parseErrorBody({ detail: 'track not found', code: 'catalog.track_not_found' })).toEqual({
      detail: 'track not found',
      code: 'catalog.track_not_found',
    });
  });

  it('omits code when absent, still returning the detail (a usable body)', () => {
    expect(parseErrorBody({ detail: 'message required' })).toEqual({ detail: 'message required' });
  });

  it('returns an empty object for a non-object, null, or array body rather than throwing', () => {
    expect(parseErrorBody(null)).toEqual({});
    expect(parseErrorBody('boom')).toEqual({});
    expect(parseErrorBody([1, 2])).toEqual({});
  });

  it('ignores a non-string code or detail rather than throwing, unlike the strict narrowers', () => {
    expect(parseErrorBody({ code: 42, detail: true })).toEqual({});
  });
});
