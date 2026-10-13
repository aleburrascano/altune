import { faker } from '@faker-js/faker';

import { FAKER_SEED } from '@testing/fakerSeed';
import { makeIdlePlayback, trackResponseFactory } from '@testing/factories';
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

describe('makeIdlePlayback', () => {
  it('builds an idle context with no track and a command for every control', () => {
    const idle = makeIdlePlayback();
    expect(idle).toMatchObject({ status: 'idle', track: null, positionMs: 0, durationMs: 0 });
    expect(typeof idle.retry).toBe('function');
    expect(typeof idle.seekTo).toBe('function');
  });

  it('replaces exactly the fields an override names', () => {
    const pause = jest.fn();
    const playing = makeIdlePlayback({ status: 'playing', pause });
    expect(playing.status).toBe('playing');
    expect(playing.pause).toBe(pause);
    expect(playing.track).toBeNull();
  });

  it('resolves the async commands', async () => {
    await expect(makeIdlePlayback().skipNext()).resolves.toBeUndefined();
  });
});
