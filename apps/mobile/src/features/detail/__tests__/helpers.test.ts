import type { DiscoveryResult } from '@shared/api-client/discovery';

import { _albumYear, compactCount, formatRuntime } from '../ui/helpers';

function album(extras: Record<string, unknown>): DiscoveryResult {
  return {
    kind: 'album',
    title: 'Album',
    subtitle: 'Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras,
  };
}

describe('compactCount', () => {
  it('leaves counts below one thousand unformatted', () => {
    expect(compactCount(999)).toBe('999');
  });

  it('formats exactly one thousand as 1.0K', () => {
    expect(compactCount(1_000)).toBe('1.0K');
  });

  it('formats exactly one million as 1.0M', () => {
    expect(compactCount(1_000_000)).toBe('1.0M');
  });

  it('formats exactly one billion as 1.0B', () => {
    expect(compactCount(1_000_000_000)).toBe('1.0B');
  });

  it('rounds within a magnitude to one decimal', () => {
    expect(compactCount(1_500)).toBe('1.5K');
  });
});

describe('formatRuntime', () => {
  it('returns null at and below zero seconds', () => {
    expect(formatRuntime(0)).toBeNull();
    expect(formatRuntime(-5)).toBeNull();
  });

  it('reports whole minutes for a sub-hour runtime', () => {
    expect(formatRuntime(125)).toBe('2 min');
  });

  it('reports hours and minutes past the hour boundary', () => {
    expect(formatRuntime(3_660)).toBe('1 hr 1 min');
  });

  it('drops the minute remainder to zero at an exact hour', () => {
    expect(formatRuntime(3_600)).toBe('1 hr 0 min');
  });
});

describe('_albumYear', () => {
  it('takes the four-digit year from the release date when present', () => {
    expect(_albumYear(album({ release_date: '1994-11-01' }))).toBe('1994');
  });

  it('falls back to the year field when no release date exists', () => {
    expect(_albumYear(album({ year: 1994 }))).toBe('1994');
  });

  it('returns null when neither release date nor year exists', () => {
    expect(_albumYear(album({}))).toBeNull();
  });
});
