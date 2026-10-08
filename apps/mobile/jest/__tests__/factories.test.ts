import { faker } from '@faker-js/faker';

import { FAKER_SEED } from '@testing/fakerSeed';
import { sessionFactory, trackResponseFactory } from '@testing/factories';
import { expectType, type IsAssignable } from '@testing/typeAssertions';
import type { Session } from '@supabase/supabase-js';
import type { TrackResponse } from '@shared/api-client/trackResponse';

describe('factories', () => {
  it('builds values typed as their models', () => {
    expectType<IsAssignable<ReturnType<typeof trackResponseFactory.build>, TrackResponse>>(true);
    expectType<IsAssignable<ReturnType<typeof sessionFactory.build>, Session>>(true);
  });

  it('replaces exactly the field an override names on a track', () => {
    const base = trackResponseFactory.build();
    const overridden = trackResponseFactory.build({ title: 'Only This' });
    expect(overridden.title).toBe('Only This');
    expect(base.title).not.toBe('Only This');
    expect(Object.keys(overridden).sort()).toEqual(Object.keys(base).sort());
  });

  it('replaces exactly the field an override names on a session', () => {
    const overridden = sessionFactory.build({ expires_in: 5 });
    expect(overridden.expires_in).toBe(5);
    expect(overridden.token_type).toBe('bearer');
    expect(overridden.access_token).toHaveLength(32);
  });

  it('builds equal values after re-seeding with FAKER_SEED', () => {
    faker.seed(FAKER_SEED);
    const firstTrack = trackResponseFactory.build();
    const firstSession = sessionFactory.build();
    faker.seed(FAKER_SEED);
    expect(trackResponseFactory.build()).toEqual(firstTrack);
    expect(sessionFactory.build()).toEqual(firstSession);
  });
});
