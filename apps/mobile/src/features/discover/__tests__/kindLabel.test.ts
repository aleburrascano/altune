import { kindLabel } from '../kindLabel';

import type { DiscoveryKind } from '@shared/api-client/discovery';

describe('kindLabel renders the user-facing kind label', () => {
  const cases: [DiscoveryKind, string, string][] = [
    ['artist', 'Artist', 'Artists'],
    ['album', 'Album', 'Albums'],
    ['track', 'Track', 'Tracks'],
  ];

  it.each(cases)('labels %s as singular by default', (kind, singular) => {
    expect(kindLabel(kind)).toBe(singular);
  });

  it.each(cases)('labels %s as singular when plural is explicitly false', (kind, singular) => {
    expect(kindLabel(kind, { plural: false })).toBe(singular);
  });

  it.each(cases)('labels %s as plural when plural is true', (kind, _singular, plural) => {
    expect(kindLabel(kind, { plural: true })).toBe(plural);
  });

  it('never surfaces the banned noun for the track kind', () => {
    expect(kindLabel('track')).not.toMatch(/song/i);
    expect(kindLabel('track', { plural: true })).not.toMatch(/song/i);
  });
});
