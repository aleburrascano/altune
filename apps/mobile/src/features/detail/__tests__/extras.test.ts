/**
 * trackInfoRows / formatDuration — present extras render in order; absent or
 * empty keys are omitted (view-result-detail slice 13, AC#3).
 *
 * Updated: ISRC and popularity removed from display per UX audit (not user-facing).
 */

import { formatDuration, trackInfoRows } from '../extras';

describe('formatDuration', () => {
  it('formats seconds as M:SS with zero-padding', () => {
    expect(formatDuration(244)).toBe('4:04');
    expect(formatDuration(9)).toBe('0:09');
    expect(formatDuration(600)).toBe('10:00');
  });
});

describe('trackInfoRows', () => {
  it('returns duration and album in order when present', () => {
    const rows = trackInfoRows({
      duration_seconds: 244,
      album: 'After Hours',
      isrc: 'USUG11904206',
      popularity: 0.72,
      preview_url: 'https://x',
    });
    expect(rows.map((r) => r.key)).toEqual(['duration', 'album']);
    expect(rows[0]?.value).toBe('4:04');
    expect(rows[1]?.value).toBe('After Hours');
  });

  it('omits absent, null, and empty values', () => {
    expect(trackInfoRows({})).toEqual([]);
    expect(trackInfoRows({ duration_seconds: null, album: '' })).toEqual([]);
    expect(trackInfoRows({ duration_seconds: 0 })).toEqual([]);
  });

  it('keeps only the present subset', () => {
    const rows = trackInfoRows({ album: 'Hurry Up', duration_seconds: 180 });
    expect(rows.map((r) => r.key)).toEqual(['duration', 'album']);
  });
});
