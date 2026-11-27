import {
  API_ERROR_CODES,
  ApiError,
  ContractError,
  NetworkError,
  failureKindOf,
  kindOfCode,
  kindOfStatus,
} from '@shared/failure';

const STATUS = {
  ok: 200,
  badRequest: 400,
  unauthorized: 401,
  forbidden: 403,
  notFound: 404,
  timeout: 408,
  conflict: 409,
  gone: 410,
  unprocessable: 422,
  tooManyRequests: 429,
  internal: 500,
  unavailable: 503,
} as const;

describe('failureKindOf', () => {
  it.each([
    [STATUS.unauthorized, 'unauthorized'],
    [STATUS.forbidden, 'unauthorized'],
    [STATUS.notFound, 'not-found'],
    [STATUS.gone, 'not-found'],
    [STATUS.timeout, 'timeout'],
    [STATUS.conflict, 'conflict'],
    [STATUS.tooManyRequests, 'rate-limited'],
    [STATUS.badRequest, 'rejected'],
    [STATUS.unprocessable, 'rejected'],
    [STATUS.internal, 'server'],
    [STATUS.unavailable, 'server'],
    [STATUS.ok, 'unknown'],
  ])('puts status %i in the %s kind', (status, kind) => {
    expect(failureKindOf(new ApiError(status, 'x'))).toBe(kind);
    expect(kindOfStatus(status)).toBe(kind);
  });

  it('gives every server error code a kind, never throwing for a known code', () => {
    for (const code of API_ERROR_CODES) expect(() => kindOfCode(code)).not.toThrow();
  });

  it.each([
    ['catalog.audio_orphaned', 'server'],
    ['discovery.provider_rate_limited', 'server'],
    ['eventstream.too_many_streams', 'rate-limited'],
    ['catalog.playlist_full', 'rejected'],
    ['request.too_large', 'limit-reached'],
    ['request.timeout', 'timeout'],
  ] as const)('gives %s the kind the server catalog assigns it: %s', (code, kind) => {
    expect(kindOfCode(code)).toBe(kind);
  });

  it('lets a known code decide over the status', () => {
    const error = new ApiError(STATUS.badRequest, 'x', 'auth.token_expired');
    expect(failureKindOf(error)).toBe('unauthorized');
  });

  it('falls back to the status for a code this build does not know, and keeps the raw code', () => {
    const error = new ApiError(STATUS.unavailable, 'x', undefined, undefined, {
      rawCode: 'brand.new_code',
    });
    expect(failureKindOf(error)).toBe('server');
    expect(error.rawCode).toBe('brand.new_code');
  });

  it('names a transport failure offline, a contract violation contract, anything else unknown', () => {
    expect(failureKindOf(new NetworkError('transport', 'x'))).toBe('offline');
    expect(failureKindOf(new ContractError('a', 'b'))).toBe('contract');
    expect(failureKindOf(new Error('x'))).toBe('unknown');
    expect(failureKindOf('boom')).toBe('unknown');
  });
});
