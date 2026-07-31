import { getLyrics, type LyricsResponse } from '../lyrics';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const filledResponse: LyricsResponse = {
  plain: 'Now here you go again\nYou say you want your freedom',
  synced_lines: [
    { timecode: '00:00.10', line: 'Now here you go again', milliseconds: 100, duration: 2000 },
  ],
  writers: ['Stevie Nicks'],
  copyright: '1977 Fleetwood Mac',
};

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockReset().mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

describe('getLyrics query construction (subtitle guard: null | undefined | "" all omit, only a real string is sent)', () => {
  it.each([
    ['undefined', undefined],
    ['null', null],
    ['empty string', ''],
  ])('omits the subtitle param when subtitle is %s', async (_label, subtitle) => {
    __http.reply('GET /v1/discovery/lyrics', { status: 200, json: filledResponse });

    await getLyrics({ title: 'Dreams', subtitle });

    expect(__http.last().query).toBe('title=Dreams');
  });

  it('includes the subtitle param when it is a real, non-empty string', async () => {
    __http.reply('GET /v1/discovery/lyrics', { status: 200, json: filledResponse });

    await getLyrics({ title: 'Dreams', subtitle: 'Fleetwood Mac' });

    expect(__http.last().query).toBe('title=Dreams&subtitle=Fleetwood+Mac');
  });
});

describe('getLyrics legacy/compat fixtures for synced_lines and writers', () => {
  it('coerces both collections to [] when the wire response omits them entirely (Go omitempty)', async () => {
    __http.reply('GET /v1/discovery/lyrics', {
      status: 200,
      json: { plain: 'la la la', copyright: '2020 Someone' },
    });

    const result = await getLyrics({ title: 'La La', subtitle: 'Someone' });

    expect(result.synced_lines).toEqual([]);
    expect(result.writers).toEqual([]);
  });

  it('coerces both collections to [] when the wire response sends them as null', async () => {
    __http.reply('GET /v1/discovery/lyrics', {
      status: 200,
      json: { plain: 'la la la', synced_lines: null, writers: null, copyright: '2020 Someone' },
    });

    const result = await getLyrics({ title: 'La La', subtitle: 'Someone' });

    expect(result.synced_lines).toEqual([]);
    expect(result.writers).toEqual([]);
  });

  it('passes through real, populated collections unchanged', async () => {
    __http.reply('GET /v1/discovery/lyrics', { status: 200, json: filledResponse });

    const result = await getLyrics({ title: 'Dreams', subtitle: 'Fleetwood Mac' });

    expect(result).toEqual(filledResponse);
  });

  it('resolves an unresolved track as an empty-but-present DTO, not an error (null-object contract)', async () => {
    __http.reply('GET /v1/discovery/lyrics', {
      status: 200,
      json: { plain: '', synced_lines: [], writers: [], copyright: '' },
    });

    const result = await getLyrics({ title: 'Some Obscure B-Side', subtitle: 'Nobody' });

    expect(result).toEqual({ plain: '', synced_lines: [], writers: [], copyright: '' });
  });
});

describe('getLyrics adversarial inbound payloads', () => {
  it('uses ?? rather than || to coerce, so a falsy-but-present empty string in synced_lines survives unchanged', async () => {
    __http.reply('GET /v1/discovery/lyrics', {
      status: 200,
      json: { plain: 'x', synced_lines: '', writers: [], copyright: '' },
    });

    const result = await getLyrics({ title: 'X', subtitle: 'Y' });

    expect(result.synced_lines).toBe('');
  });

  it('leaves synced_lines untouched when the server sends a string instead of an array (no shape validation)', async () => {
    __http.reply('GET /v1/discovery/lyrics', {
      status: 200,
      json: { plain: 'x', synced_lines: 'not-an-array', writers: [], copyright: '' },
    });

    const result = await getLyrics({ title: 'X', subtitle: 'Y' });

    expect(result.synced_lines).toBe('not-an-array');
  });

  it('rejects with a TypeError when the wire body is JSON null (unguarded-wire-cast contract, not a safe-degradation promise)', async () => {
    __http.reply('GET /v1/discovery/lyrics', { status: 200, json: null });

    await expect(getLyrics({ title: 'X', subtitle: 'Y' })).rejects.toBeInstanceOf(TypeError);
  });

  it('rejects with a TypeError on a 204 short-circuit, since apiFetch resolves undefined and the coercion reads a property off it (unguarded-wire-cast contract)', async () => {
    __http.reply('GET /v1/discovery/lyrics', { status: 204 });

    await expect(getLyrics({ title: 'X', subtitle: 'Y' })).rejects.toBeInstanceOf(TypeError);
  });

  it('spreads an array response into a numerically-keyed object rather than throwing, still coercing the two collections', async () => {
    __http.reply('GET /v1/discovery/lyrics', { status: 200, json: [1, 2, 3] });

    const result = await getLyrics({ title: 'X', subtitle: 'Y' });

    expect(result).toMatchObject({ 0: 1, 1: 2, 2: 3, synced_lines: [], writers: [] });
  });
});
