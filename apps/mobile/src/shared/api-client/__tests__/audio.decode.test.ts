import { fetchAudioUrls, isAudioPrefetchEnabled } from '../audio';
import { ContractError } from '@shared/errors';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const VALID_ENTRY = { track_id: 't1', url: 'https://cdn.example/t1.mp3', version: 'v1' };

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

function replyWith(body: unknown): void {
  __http.replyOnce('POST /v1/audio-urls', { status: 200, json: body });
}

function replyWithEntry(overrides: Record<string, unknown>): void {
  replyWith({ urls: [{ ...VALID_ENTRY, ...overrides }] });
}

describe('fetchAudioUrls decodes the response instead of trusting its shape', () => {
  it.each([
    ['a null body', null],
    ['an array body', []],
    ['a missing urls list', {}],
    ['a null urls list', { urls: null }],
    ['a urls object instead of a list', { urls: { track_id: 't1' } }],
    ['a null entry', { urls: [null] }],
    ['a string entry', { urls: ['https://cdn.example/t1.mp3'] }],
  ])('rejects %s with a ContractError, not a TypeError', async (_label, body) => {
    replyWith(body);

    await expect(fetchAudioUrls(['t1'])).rejects.toBeInstanceOf(ContractError);
  });

  it.each([
    'http://cdn.example/t1.mp3',
    'file:///data/user/0/app/files/secret.db',
    'content://media/external/audio/1',
    'javascript:alert(1)',
    'https://',
    'https://cdn.example/t1 .mp3',
    'https://cdn.example/t1\n.mp3',
    ' https://cdn.example/t1.mp3',
    '',
  ])('refuses a url %p that is not a plain https link', async (url) => {
    replyWithEntry({ url });

    await expect(fetchAudioUrls(['t1'])).rejects.toThrow(
      new ContractError('audio-urls.urls[0].url', 'expected an https url'),
    );
  });

  it.each([42, null, undefined, { href: 'https://cdn.example/t1.mp3' }])(
    'refuses a non-string url %p',
    async (url) => {
      replyWithEntry({ url });

      await expect(fetchAudioUrls(['t1'])).rejects.toBeInstanceOf(ContractError);
    },
  );

  it.each(['__proto__', 'constructor', 'prototype', '../t1', 'a/b', 'a.b', '', 'x'.repeat(129)])(
    'refuses a track_id %p that is unsafe to key a record or build a path with',
    async (trackId) => {
      replyWithEntry({ track_id: trackId });

      await expect(fetchAudioUrls(['t1'])).rejects.toThrow(
        new ContractError('audio-urls.urls[0].track_id', 'not a valid id shape'),
      );
    },
  );

  it.each([7, null, undefined, ['t1']])('refuses a non-string track_id %p', async (trackId) => {
    replyWithEntry({ track_id: trackId });

    await expect(fetchAudioUrls(['t1'])).rejects.toBeInstanceOf(ContractError);
  });

  it.each([1770000000000, true, { v: 1 }])('refuses a non-string version %p', async (version) => {
    replyWithEntry({ version });

    await expect(fetchAudioUrls(['t1'])).rejects.toBeInstanceOf(ContractError);
  });

  it('reads a null version as the unknown version, like an omitted one', async () => {
    replyWithEntry({ version: null });

    await expect(fetchAudioUrls(['t1'])).resolves.toEqual([
      { trackId: 't1', url: 'https://cdn.example/t1.mp3', version: '' },
    ]);
  });

  it('rejects the whole batch when one entry is bad, pointing at that entry', async () => {
    replyWith({ urls: [VALID_ENTRY, { ...VALID_ENTRY, track_id: 't2', url: 'http://x/t2' }] });

    await expect(fetchAudioUrls(['t1', 't2'])).rejects.toThrow(
      new ContractError('audio-urls.urls[1].url', 'expected an https url'),
    );
  });

  it('accepts an https url in any letter case', async () => {
    replyWithEntry({ url: 'HTTPS://cdn.example/t1.mp3?X-Amz-Signature=abc%2F' });

    await expect(fetchAudioUrls(['t1'])).resolves.toEqual([
      { trackId: 't1', url: 'HTTPS://cdn.example/t1.mp3?X-Amz-Signature=abc%2F', version: 'v1' },
    ]);
  });

  it('leaves the prefetch switch untouched when the body fails to decode', async () => {
    replyWith({ urls: [], prefetch_enabled: false });
    await fetchAudioUrls(['t1']);
    replyWith({ urls: null, prefetch_enabled: true });

    await expect(fetchAudioUrls(['t1'])).rejects.toBeInstanceOf(ContractError);
    expect(isAudioPrefetchEnabled()).toBe(false);
  });
});
