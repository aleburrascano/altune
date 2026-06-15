import type { DiscoveryResult } from '@shared/api-client/discovery';

import { _albumYear } from '../ui/helpers';

function album(extras: Record<string, unknown> = {}): DiscoveryResult {
  return {
    kind: 'album',
    title: 'Test Album',
    subtitle: 'Test Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras,
  };
}

describe('_albumYear', () => {
  it('extracts year from release_date string', () => {
    expect(_albumYear(album({ release_date: '2024-03-15' }))).toBe('2024');
  });

  it('extracts year from numeric year extra', () => {
    expect(_albumYear(album({ year: 2022 }))).toBe('2022');
  });

  it('extracts year from string year extra', () => {
    expect(_albumYear(album({ year: '2019' }))).toBe('2019');
  });

  it('prefers release_date over year', () => {
    expect(_albumYear(album({ release_date: '2024-01-01', year: 2019 }))).toBe('2024');
  });

  it('returns null when no date info exists', () => {
    expect(_albumYear(album({}))).toBeNull();
  });

  it('returns null for non-string/non-number extras', () => {
    expect(_albumYear(album({ year: true }))).toBeNull();
  });
});
