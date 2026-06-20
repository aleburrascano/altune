/**
 * Parity tests for normalizeForDedup against the backend's normalize_for_match.
 * Each test case verifies the TS output matches the Python output for the same input.
 */
import { normalizeForDedup } from '../helpers/normalize-for-dedup';

describe('normalizeForDedup', () => {
  it('lowercases', () => {
    expect(normalizeForDedup('HELLO WORLD')).toBe('hello world');
  });

  it('strips diacritics', () => {
    expect(normalizeForDedup('Café Del Mar')).toBe('cafe del mar');
    expect(normalizeForDedup('Beyoncé')).toBe('beyonce');
    expect(normalizeForDedup('Señorita')).toBe('senorita');
  });

  it('removes bracketed suffixes', () => {
    expect(normalizeForDedup('Song (Deluxe Edition)')).toBe('song');
    expect(normalizeForDedup('Song [Remastered]')).toBe('song');
    expect(normalizeForDedup('Song {Live}')).toBe('song');
  });

  it('normalizes feature tokens before bracket removal', () => {
    expect(normalizeForDedup('Song feat. Artist')).toBe('song feat artist');
    expect(normalizeForDedup('Song ft. Artist')).toBe('song feat artist');
    expect(normalizeForDedup('Song featuring Artist')).toBe('song feat artist');
  });

  it('strips leading articles', () => {
    expect(normalizeForDedup('The Beatles')).toBe('beatles');
    expect(normalizeForDedup('Los Lobos')).toBe('lobos');
    expect(normalizeForDedup('Les Paul')).toBe('paul');
  });

  it('strips leading article even from "The The" (parity with backend)', () => {
    expect(normalizeForDedup('The The')).toBe('the');
  });

  it('normalizes ampersand to and', () => {
    expect(normalizeForDedup('Simon & Garfunkel')).toBe('simon and garfunkel');
  });

  it('strips apostrophes and periods without gaps', () => {
    expect(normalizeForDedup("Rock 'n' Roll")).toBe('rock n roll');
    expect(normalizeForDedup('Dr. Dre')).toBe('dr dre');
    expect(normalizeForDedup("it's")).toBe('its');
  });

  it('strips curly right-quote apostrophe (U+2019)', () => {
    expect(normalizeForDedup('it’s a test')).toBe('its a test');
  });

  it('replaces other punctuation with space', () => {
    expect(normalizeForDedup('AC/DC')).toBe('ac dc');
    expect(normalizeForDedup('a-b-c')).toBe('a b c');
  });

  it('collapses whitespace and trims', () => {
    expect(normalizeForDedup('  hello   world  ')).toBe('hello world');
  });

  it('handles combined transformations', () => {
    expect(normalizeForDedup('The Café (Deluxe) feat. DJ')).toBe('cafe feat dj');
  });

  it('handles empty string', () => {
    expect(normalizeForDedup('')).toBe('');
  });
});
