const { RuleTester } = require('eslint');
const { describe, it } = require('node:test');
const tsParser = require('@typescript-eslint/parser');
const rule = require('../no-banned-noun');

RuleTester.describe = describe;
RuleTester.it = it;
RuleTester.itOnly = it.only;

const ruleTester = new RuleTester({ languageOptions: { parser: tsParser } });

const flagged = (word) => [{ messageId: 'bannedNoun', data: { word } }];

ruleTester.run('no-banned-noun', rule, {
  valid: [
    { code: 'const track = current;' },
    { code: 'const trackId = current.id;' },
    { code: 'const songwriter = current.credit;' },
    { code: "const label = 'Songwriter credits';" },
    { code: 'const label = `the songbird ${name}`;' },
    { code: 'const SONGBOOK_URL = base;' },
    { code: 'const isong = 1;' },
    { code: 'const songbird = "decoy";' },
  ],
  invalid: [
    { code: 'const song = current;', errors: flagged('song') },
    { code: 'const songId = current.id;', errors: flagged('song') },
    { code: 'const nowPlayingSong = current;', errors: flagged('Song') },
    { code: 'type SongCard = { id: string };', errors: flagged('Song') },
    { code: 'const MAX_SONG_COUNT = 10;', errors: flagged('SONG') },
    { code: 'const songs = queue.items;', errors: flagged('songs') },
    { code: 'const likedSongs = queue.items;', errors: flagged('Songs') },
    { code: 'const SONGS = [];', errors: flagged('SONGS') },
    { code: "const label = 'Play song';", errors: flagged('song') },
    { code: 'const label = `${count} songs`;', errors: flagged('songs') },
    { code: 'const view = current.songList;', errors: flagged('song') },
    { code: 'type SongTitle = string;', errors: flagged('Song') },
    { code: 'const label = "legacy songs table import";', errors: flagged('songs') },
  ],
});
