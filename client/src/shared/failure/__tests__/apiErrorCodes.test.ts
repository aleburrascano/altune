import { API_ERROR_CODES, isApiErrorCode } from '@shared/failure';

describe('isApiErrorCode', () => {
  it.each(API_ERROR_CODES)('accepts the catalog code %s', (code) => {
    expect(isApiErrorCode(code)).toBe(true);
  });

  it.each<unknown>([
    '',
    'discovery.unheard_of',
    'INTERNAL',
    true,
    null,
    undefined,
    {},
    ['internal'],
  ])('rejects %p', (value) => {
    expect(isApiErrorCode(value)).toBe(false);
  });
});
