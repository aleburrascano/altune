import { faker } from '@faker-js/faker';

import { FAKER_SEED } from '@testing/fakerSeed';
import { trackResponseFactory } from '@testing/factories';
import { expectType, type IsAssignable } from '@testing/typeAssertions';
import type { TrackResponse } from '@shared/apiClient/trackResponse';

describe('factories', () => {
  it('builds values typed as their models', () => {
    expectType<IsAssignable<ReturnType<typeof trackResponseFactory.build>, TrackResponse>>(true);
  });

  it('replaces exactly the field an override names on a track', () => {
    const base = trackResponseFactory.build();
    const overridden = trackResponseFactory.build({ title: 'Only This' });
    expect(overridden.title).toBe('Only This');
    expect(base.title).not.toBe('Only This');
    expect(Object.keys(overridden).sort()).toEqual(Object.keys(base).sort());
  });

  it('builds equal values after re-seeding with FAKER_SEED', () => {
    faker.seed(FAKER_SEED);
    const firstTrack = trackResponseFactory.build();
    faker.seed(FAKER_SEED);
    expect(trackResponseFactory.build()).toEqual(firstTrack);
  });
});
