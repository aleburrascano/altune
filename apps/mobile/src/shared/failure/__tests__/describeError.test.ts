import { ApiError, ContractError } from '@shared/errors';

import { RETRY_TAIL, describeError } from '@shared/failure/describeError';
import * as libShim from '@shared/lib/describeError';

describe('describeError — network vs 5xx vs generic', () => {
  it('maps a transport failure to the offline copy', () => {
    expect(describeError(new Error('network request failed'))).toEqual({
      title: 'No connection',
      body: 'Check your connection and try again.',
    });
  });

  it('maps a 5xx ApiError to the server copy with the shared retry tail', () => {
    expect(describeError(new ApiError(500, 'Internal error'))).toEqual({
      title: 'Something went wrong',
      body: `Something went wrong on our end. ${RETRY_TAIL}`,
    });
  });

  it('maps a 429 ApiError to a rate-limit copy that asks the user to wait', () => {
    expect(describeError(new ApiError(429, 'Too many requests'))).toEqual({
      title: 'Too many requests',
      body: 'You are doing that too often. Wait a moment, then try again.',
    });
  });

  it('maps a permanent 4xx ApiError to a copy with no retry tail', () => {
    const copy = describeError(new ApiError(400, 'Bad request'));

    expect(copy).toEqual({
      title: 'Something went wrong',
      body: 'We could not complete that request.',
    });
    expect(copy.body).not.toMatch(/try again/i);
  });

  it('maps an Error without a status to the generic copy', () => {
    expect(describeError(new Error('unexpected'))).toEqual({
      title: 'Something went wrong',
      body: `Something went wrong. ${RETRY_TAIL}`,
    });
  });

  it('maps a non-Error thrown value to the generic copy', () => {
    expect(describeError('boom')).toEqual({
      title: 'Something went wrong',
      body: `Something went wrong. ${RETRY_TAIL}`,
    });
  });
});

describe('describeError — a query string cannot masquerade as a transport failure', () => {
  it('maps a 5xx ApiError whose path contains "network" to the server copy', () => {
    expect(
      describeError(new ApiError(500, 'API /v1/discovery/search?q=network returned 500')),
    ).toEqual({
      title: 'Something went wrong',
      body: `Something went wrong on our end. ${RETRY_TAIL}`,
    });
  });
});

describe('describeError — a response this build can no longer decode', () => {
  it('maps a ContractError to an update prompt instead of the generic copy', () => {
    expect(describeError(new ContractError('GET /v1/tracks', 'expected an object'))).toEqual({
      title: 'Update required',
      body: 'This version of Altune is out of date. Update the app to continue.',
    });
  });

  it('never asks a ContractError to be retried — no retry can reach a shape this build reads', () => {
    const { body } = describeError(new ContractError('GET /v1/tracks', 'expected an object'));

    expect(body).not.toContain(RETRY_TAIL);
  });

  it('maps a ContractError whose detail quotes an enum containing "timeout" to the update prompt', () => {
    const rejectedEnumValue = new ContractError(
      'discovery.providers[0].status',
      'not one of ok, timeout, error, rate_limited, circuit_open',
    );

    expect(describeError(rejectedEnumValue).title).toBe('Update required');
  });

  it('does not classify a non-Error object merely named ContractError', () => {
    expect(describeError({ name: 'ContractError', message: 'x' })).toEqual({
      title: 'Something went wrong',
      body: `Something went wrong. ${RETRY_TAIL}`,
    });
  });
});

describe('RETRY_TAIL', () => {
  it('is the single source of the retry sentence', () => {
    expect(RETRY_TAIL).toBe('Please try again.');
  });
});

describe('describeError — lib shim', () => {
  const HTTP_BAD_REQUEST = 400;

  it('re-exports the same objects as the failure path', () => {
    expect(libShim.describeError).toBe(describeError);
    expect(libShim.RETRY_TAIL).toBe(RETRY_TAIL);
  });

  it('refuses an ApiError whose message looks like a network failure, through both paths', () => {
    const err = new ApiError(HTTP_BAD_REQUEST, 'network down');
    expect(describeError(err).title).not.toBe('No connection');
    expect(libShim.describeError(err).title).not.toBe('No connection');
  });
});
