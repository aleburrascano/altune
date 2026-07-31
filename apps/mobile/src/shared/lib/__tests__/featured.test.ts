import fc from 'fast-check';

import { featuredArtistsFromExtras, withFeaturing } from '../featured';

import type { FeaturedArtist } from '@shared/api-client/types';

describe('featuredArtistsFromExtras — Array.isArray guard', () => {
  it.each<unknown>(['not-an-array', 42, true, {}, null, undefined])(
    'returns [] for a non-array raw value: %p',
    (raw) => {
      expect(featuredArtistsFromExtras(raw)).toEqual([]);
    },
  );

  it('returns [] for an empty array', () => {
    expect(featuredArtistsFromExtras([])).toEqual([]);
  });
});

describe('featuredArtistsFromExtras — the string arm (no known live producer, still constrained)', () => {
  it('drops an empty string exactly at the length > 0 boundary', () => {
    expect(featuredArtistsFromExtras(['SZA', ''])).toEqual([{ name: 'SZA', mbid: null, deezer_id: null }]);
  });

  it('accepts a non-empty bare string as a name-only FeaturedArtist', () => {
    expect(featuredArtistsFromExtras(['SZA'])).toEqual([{ name: 'SZA', mbid: null, deezer_id: null }]);
  });
});

describe('featuredArtistsFromExtras — object arm: item !== null && typeof item === "object" guard', () => {
  it('skips a null entry in the array', () => {
    expect(featuredArtistsFromExtras([null, { name: 'Rihanna' }])).toEqual([
      { name: 'Rihanna', mbid: null, deezer_id: null },
    ]);
  });

  it('skips a primitive number entry (neither string nor object)', () => {
    expect(featuredArtistsFromExtras([42, { name: 'Rihanna' }])).toEqual([
      { name: 'Rihanna', mbid: null, deezer_id: null },
    ]);
  });

  it('skips a boolean entry', () => {
    expect(featuredArtistsFromExtras([true, { name: 'Rihanna' }])).toEqual([
      { name: 'Rihanna', mbid: null, deezer_id: null },
    ]);
  });
});

describe('featuredArtistsFromExtras — name type guard and the length === 0 boundary', () => {
  it('treats a non-string name as absent, filtering the entry out', () => {
    expect(featuredArtistsFromExtras([{ name: 42 }])).toEqual([]);
  });

  it('treats a null name as absent, filtering the entry out', () => {
    expect(featuredArtistsFromExtras([{ name: null }])).toEqual([]);
  });

  it('filters an entry whose name is exactly the empty string', () => {
    expect(featuredArtistsFromExtras([{ name: '' }])).toEqual([]);
  });

  it('keeps an entry whose name is a single non-empty character', () => {
    expect(featuredArtistsFromExtras([{ name: 'X' }])).toEqual([
      { name: 'X', mbid: null, deezer_id: null },
    ]);
  });
});

describe('featuredArtistsFromExtras — mbid type guard, independent of the other fields', () => {
  it('keeps a string mbid', () => {
    expect(featuredArtistsFromExtras([{ name: 'SZA', mbid: 'mb-1' }])[0]).toEqual({
      name: 'SZA',
      mbid: 'mb-1',
      deezer_id: null,
    });
  });

  it('nulls a non-string mbid (number)', () => {
    expect(featuredArtistsFromExtras([{ name: 'SZA', mbid: 123 }])[0]!.mbid).toBeNull();
  });

  it('nulls a missing mbid key', () => {
    expect(featuredArtistsFromExtras([{ name: 'SZA' }])[0]!.mbid).toBeNull();
  });
});

describe('featuredArtistsFromExtras — deezer_id type guard, independent of the other fields', () => {
  it('keeps a numeric deezer_id', () => {
    expect(featuredArtistsFromExtras([{ name: 'SZA', deezer_id: 564 }])[0]).toEqual({
      name: 'SZA',
      mbid: null,
      deezer_id: 564,
    });
  });

  it('nulls a numeric-string deezer_id ("123" is not typeof number)', () => {
    expect(featuredArtistsFromExtras([{ name: 'SZA', deezer_id: '123' }])[0]!.deezer_id).toBeNull();
  });

  it('nulls a missing deezer_id key', () => {
    expect(featuredArtistsFromExtras([{ name: 'SZA' }])[0]!.deezer_id).toBeNull();
  });
});

describe('featuredArtistsFromExtras — adversarial: malformed, tampered and thin payloads off the wire', () => {
  it('does not throw on an array mixing null, numbers, booleans, nested arrays and functions', () => {
    const raw: unknown[] = [null, 1, false, ['nested'], () => {}, { name: 'Rihanna' }];
    expect(() => featuredArtistsFromExtras(raw)).not.toThrow();
    expect(featuredArtistsFromExtras(raw)).toEqual([{ name: 'Rihanna', mbid: null, deezer_id: null }]);
  });

  it('skips a nested-array entry rather than reading it as a named credit', () => {
    expect(featuredArtistsFromExtras([['SZA']])).toEqual([]);
  });

  it('skips a function entry', () => {
    const raw: unknown[] = [() => 'SZA'];
    expect(featuredArtistsFromExtras(raw)).toEqual([]);
  });

  it('rejects a NaN deezer_id rather than letting typeof "number" admit it', () => {
    const out = featuredArtistsFromExtras([{ name: 'SZA', deezer_id: NaN }]);
    expect(out[0]!.deezer_id).toBeNull();
  });

  it.each<[string, number]>([
    ['Infinity', Infinity],
    ['-Infinity', -Infinity],
  ])('rejects a non-finite deezer_id of %s', (_label, value) => {
    const out = featuredArtistsFromExtras([{ name: 'SZA', deezer_id: value }]);
    expect(out[0]!.deezer_id).toBeNull();
  });

  it('does not throw and does not pollute Object.prototype on an entry carrying a __proto__ own key', () => {
    const raw: unknown[] = JSON.parse('[{"name":"safe","__proto__":{"polluted":true}}]');
    expect(() => featuredArtistsFromExtras(raw)).not.toThrow();
    const out = featuredArtistsFromExtras(raw);
    expect(out).toEqual([{ name: 'safe', mbid: null, deezer_id: null }]);
    expect(({} as Record<string, unknown>)['polluted']).toBeUndefined();
  });
});

describe('featuredArtistsFromExtras — legacy/compat: historical shapes still in the wild', () => {
  it('an entry missing mbid entirely (Go omits rather than nulling) nulls mbid', () => {
    const raw = JSON.parse('[{"name":"Destroy Lonely","role":"featured","deezer_id":99}]');
    expect(featuredArtistsFromExtras(raw)).toEqual([
      { name: 'Destroy Lonely', mbid: null, deezer_id: 99 },
    ]);
  });

  it('an entry missing deezer_id entirely nulls deezer_id', () => {
    const raw = JSON.parse('[{"name":"SZA","role":"featured","mbid":"mb-1"}]');
    expect(featuredArtistsFromExtras(raw)).toEqual([{ name: 'SZA', mbid: 'mb-1', deezer_id: null }]);
  });

  it('an entry carrying the "role" key the TS FeaturedArtist type has no field for is parsed without it', () => {
    const raw = JSON.parse('[{"name":"SZA","role":"featured","mbid":"mb-1","deezer_id":7}]');
    const out = featuredArtistsFromExtras(raw);
    expect(out).toEqual([{ name: 'SZA', mbid: 'mb-1', deezer_id: 7 }]);
    expect(Object.keys(out[0]!).sort()).toEqual(['deezer_id', 'mbid', 'name']);
  });

  it('featured_artists absent from extras entirely resolves to []', () => {
    const extras: Record<string, unknown> = {};
    expect(featuredArtistsFromExtras(extras['featured_artists'])).toEqual([]);
  });

  it('featured_artists explicitly null resolves to []', () => {
    const extras: Record<string, unknown> = { featured_artists: null };
    expect(featuredArtistsFromExtras(extras['featured_artists'])).toEqual([]);
  });

  it('featured_artists as an empty array resolves to []', () => {
    const extras: Record<string, unknown> = { featured_artists: [] };
    expect(featuredArtistsFromExtras(extras['featured_artists'])).toEqual([]);
  });
});

describe('featuredArtistsFromExtras — idempotence / replay', () => {
  it('applying the parser to its own output returns a deep-equal array', () => {
    const raw = [
      { name: 'SZA', mbid: 'mb-1', deezer_id: 7 },
      { name: 'Rihanna', mbid: null, deezer_id: 564 },
    ];
    const first = featuredArtistsFromExtras(raw);
    const second = featuredArtistsFromExtras(first);
    expect(second).toEqual(first);
  });
});

describe('law: featuredArtistsFromExtras over arbitrary array input', () => {
  const referenceKeys = Object.keys(
    featuredArtistsFromExtras([{ name: 'Probe', mbid: 'm', deezer_id: 1 }])[0]!,
  ).sort();

  const arbEntry = fc.oneof(
    fc.record({
      name: fc.oneof(fc.string(), fc.integer(), fc.constant(null), fc.boolean()),
      mbid: fc.oneof(fc.string(), fc.integer(), fc.constant(null)),
      deezer_id: fc.oneof(fc.integer(), fc.string(), fc.constant(null)),
    }),
    fc.string(),
    fc.constant(null),
    fc.integer(),
    fc.boolean(),
    fc.array(fc.string(), { maxLength: 2 }),
  );
  const arbRaw = fc.array(arbEntry, { maxLength: 10 });

  it('never produces more entries than the input array had', () => {
    fc.assert(
      fc.property(arbRaw, (raw) => {
        expect(featuredArtistsFromExtras(raw).length).toBeLessThanOrEqual(raw.length);
      }),
    );
  });

  it('never produces an entry with an empty name', () => {
    fc.assert(
      fc.property(arbRaw, (raw) => {
        for (const entry of featuredArtistsFromExtras(raw)) {
          expect(entry.name.length).toBeGreaterThan(0);
        }
      }),
    );
  });

  it('every entry has exactly the FeaturedArtist keys, no more, no fewer', () => {
    fc.assert(
      fc.property(arbRaw, (raw) => {
        for (const entry of featuredArtistsFromExtras(raw)) {
          expect(Object.keys(entry).sort()).toEqual(referenceKeys);
        }
      }),
    );
  });

  it('round-trips: feeding its own output back through returns an equal array', () => {
    fc.assert(
      fc.property(arbRaw, (raw) => {
        const out = featuredArtistsFromExtras(raw);
        expect(featuredArtistsFromExtras(out)).toEqual(out);
      }),
    );
  });
});

describe('withFeaturing — table over undefined / empty / one / several credits', () => {
  it('returns the base string unchanged when featured is undefined', () => {
    expect(withFeaturing('Drake', undefined)).toBe('Drake');
  });

  it('returns the base string unchanged when featured is []', () => {
    expect(withFeaturing('Drake', [])).toBe('Drake');
  });

  it('appends a single featured credit', () => {
    const featured: FeaturedArtist[] = [{ name: 'Rihanna', mbid: null, deezer_id: null }];
    expect(withFeaturing('Drake', featured)).toBe('Drake, Rihanna');
  });

  it('appends several featured credits in order', () => {
    const featured: FeaturedArtist[] = [
      { name: 'Rihanna', mbid: null, deezer_id: null },
      { name: 'SZA', mbid: null, deezer_id: null },
    ];
    expect(withFeaturing('Drake', featured)).toBe('Drake, Rihanna, SZA');
  });
});

describe('withFeaturing — functional: the co-billed secondary line the app promises', () => {
  it('renders "Artist, Guest, Guest" for a Track carrying two FeaturedArtist credits, as playback and detail surfaces show it', () => {
    const featured: FeaturedArtist[] = [
      { name: 'Guest', mbid: 'mb-guest', deezer_id: null },
      { name: 'Guest', mbid: null, deezer_id: 12 },
    ];
    expect(withFeaturing('Artist', featured)).toBe('Artist, Guest, Guest');
  });

  it('renders the base Track artist alone when it carries no FeaturedArtist credits', () => {
    expect(withFeaturing('Solo Artist', [])).toBe('Solo Artist');
  });
});
