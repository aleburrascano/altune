import { asTrackId, type TrackId } from '@shared/apiClient/ids';
import { signInTestUser } from '@testing/signInTestUser';
import { ApiError, ContractError } from '@shared/failure/errors';

import { httpAcquisitionPort } from '@shared/acquisition/adapters/http/httpAcquisitionPort';
import { createMemoryAcquisitionPort } from '@shared/acquisition/__tests__/memoryAcquisitionPort';
import type { AcquisitionPort } from '@shared/acquisition/ports/acquisitionPort';

const { __http, fakeFetch } = require('@testing/doubles/fetch.js');

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));

const RETRY_PATH_PATTERN = /^\/v1\/tracks\/([^/]+)\/retry$/;
const NO_CONTENT_STATUS = 204;
const NOT_FOUND_STATUS = 404;
const LOOK_ALIKE_ID = 't1 ' as TrackId;

interface SeededPort {
  port: AcquisitionPort;
  retried: () => readonly string[];
}

function serveAcquisitionOverHttp(trackIds: readonly string[]): SeededPort {
  const retried: string[] = [];
  global.fetch = ((url: string, init?: RequestInit) => {
    const path = String(url)
      .replace(/^[a-z]+:\/\/[^/]+/, '')
      .split('?')[0] as string;
    const method = String(init?.method ?? 'GET').toUpperCase();
    const encodedId = RETRY_PATH_PATTERN.exec(path)?.[1];
    if (method === 'POST' && encodedId !== undefined) {
      const id = decodeURIComponent(encodedId);
      if (trackIds.includes(id)) {
        retried.push(id);
        __http.replyOnce(`POST ${path}`, { status: NO_CONTENT_STATUS });
      } else {
        __http.replyOnce(`POST ${path}`, { status: NOT_FOUND_STATUS, json: {} });
      }
    }
    return fakeFetch(url, init);
  }) as typeof fetch;
  return { port: httpAcquisitionPort, retried: () => retried };
}

function seedMemory(trackIds: readonly string[]): SeededPort {
  const memory = createMemoryAcquisitionPort({ trackIds });
  return { port: memory.port, retried: () => memory.retried };
}

beforeEach(async () => {
  __http.reset();
  await signInTestUser();
});

afterEach(() => {
  global.fetch = fakeFetch;
});

describe.each<[string, (trackIds: readonly string[]) => SeededPort]>([
  ['createMemoryAcquisitionPort', seedMemory],
  ['httpAcquisitionPort over HTTP', serveAcquisitionOverHttp],
])('%s satisfies the AcquisitionPort contract', (_name, seeded) => {
  it('resolves when retrying a seeded track', async () => {
    const { port, retried } = seeded(['t1']);

    await expect(port.retryAcquisition(asTrackId('t1'))).resolves.toBeUndefined();

    expect(retried()).toEqual(['t1']);
  });

  it('resolves again when the same track is retried a second time', async () => {
    const { port, retried } = seeded(['t1']);

    await port.retryAcquisition(asTrackId('t1'));
    await expect(port.retryAcquisition(asTrackId('t1'))).resolves.toBeUndefined();

    expect(retried()).toEqual(['t1', 't1']);
  });

  it('rejects with a 404 ApiError for an id that is not seeded', async () => {
    const { port, retried } = seeded(['t1']);

    const failure = await port.retryAcquisition(asTrackId('t2')).catch((error: unknown) => error);

    expect(failure).toBeInstanceOf(ApiError);
    expect((failure as ApiError).status).toBe(NOT_FOUND_STATUS);
    expect(retried()).toEqual([]);
  });

  it('refuses the look-alike id "t1 " when only "t1" is seeded', async () => {
    const { port, retried } = seeded(['t1']);

    await expect(port.retryAcquisition(LOOK_ALIKE_ID)).rejects.toBeInstanceOf(Error);

    expect(retried()).toEqual([]);
  });

  it('rejects every retry with 404 when nothing is seeded', async () => {
    const { port } = seeded([]);

    for (const id of ['t1', 't2']) {
      const failure = await port.retryAcquisition(asTrackId(id)).catch((error: unknown) => error);
      expect(failure).toBeInstanceOf(ApiError);
      expect((failure as ApiError).status).toBe(NOT_FOUND_STATUS);
    }
  });
});

describe('look-alike id refusal, per implementation', () => {
  it('memory fake answers the look-alike with a 404 ApiError', async () => {
    const { port } = seedMemory(['t1']);

    const failure = await port.retryAcquisition(LOOK_ALIKE_ID).catch((error: unknown) => error);

    expect((failure as ApiError).status).toBe(NOT_FOUND_STATUS);
  });

  it('http adapter refuses the look-alike client-side with a ContractError, sending nothing', async () => {
    const { port } = serveAcquisitionOverHttp(['t1']);

    await expect(port.retryAcquisition(LOOK_ALIKE_ID)).rejects.toBeInstanceOf(ContractError);

    expect(__http.requests).toHaveLength(0);
  });
});

describe('httpAcquisitionPort wire format', () => {
  it('sends POST /v1/tracks/t1/retry', async () => {
    const { port } = serveAcquisitionOverHttp(['t1']);

    await port.retryAcquisition(asTrackId('t1'));

    expect(__http.last()).toMatchObject({ method: 'POST', path: '/v1/tracks/t1/retry' });
  });
});
