import { getLyrics, type LyricsResponse } from '../lyrics';
import { ContractError } from '@shared/errors';
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
  it.each([
    ['an empty string', ''],
    ['a string', 'not-an-array'],
    ['an object', { 0: 'line' }],
  ])(
    'rejects with a ContractError when synced_lines is %s, rather than handing the lyrics sheet a non-list to map over',
    async (_label, syncedLines) => {
      __http.reply('GET /v1/discovery/lyrics', {
        status: 200,
        json: { plain: 'x', synced_lines: syncedLines, writers: [], copyright: '' },
      });

      await expect(getLyrics({ title: 'X', subtitle: 'Y' })).rejects.toMatchObject({
        name: 'ContractError',
        at: 'LyricsResponse.synced_lines',
      });
    },
  );

  it('rejects with a ContractError naming the off-contract line, not the whole collection', async () => {
    __http.reply('GET /v1/discovery/lyrics', {
      status: 200,
      json: {
        plain: 'x',
        synced_lines: [{ timecode: '00:00.10', line: 'a', milliseconds: '100', duration: 2000 }],
        writers: [],
        copyright: '',
      },
    });

    await expect(getLyrics({ title: 'X', subtitle: 'Y' })).rejects.toMatchObject({
      name: 'ContractError',
      at: 'LyricsResponse.synced_lines[0].milliseconds',
    });
  });

  it.each([
    ['plain', { plain: null, synced_lines: [], writers: [], copyright: '' }],
    ['copyright', { plain: 'x', synced_lines: [], writers: [], copyright: 42 }],
  ])(
    'rejects with a ContractError when %s is not a string, rather than rendering it',
    async (field, json) => {
      __http.reply('GET /v1/discovery/lyrics', { status: 200, json });

      await expect(getLyrics({ title: 'X', subtitle: 'Y' })).rejects.toMatchObject({
        name: 'ContractError',
        at: `LyricsResponse.${field}`,
      });
    },
  );

  it('rejects with a ContractError when a writer is not a string', async () => {
    __http.reply('GET /v1/discovery/lyrics', {
      status: 200,
      json: { plain: 'x', synced_lines: [], writers: ['Stevie Nicks', null], copyright: '' },
    });

    await expect(getLyrics({ title: 'X', subtitle: 'Y' })).rejects.toMatchObject({
      name: 'ContractError',
      at: 'LyricsResponse.writers[1]',
    });
  });

  it.each([
    ['the wire body is JSON null', { status: 200, json: null }],
    ['the response is an array', { status: 200, json: [1, 2, 3] }],
    ['a 204 short-circuits the body entirely', { status: 204 }],
  ])('rejects with a ContractError when %s', async (_label, reply) => {
    __http.reply('GET /v1/discovery/lyrics', reply);

    await expect(getLyrics({ title: 'X', subtitle: 'Y' })).rejects.toBeInstanceOf(ContractError);
  });
});
