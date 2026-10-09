import { expectType, type IsAssignable, type Not } from '@testing/typeAssertions';

import type { RepeatMode } from '@shared/playback/queueSource';

describe('queueSource', () => {
  it('RepeatMode accepts off, all and one and refuses shuffle', () => {
    expectType<IsAssignable<'off' | 'all' | 'one', RepeatMode>>();
    expectType<Not<IsAssignable<'shuffle', RepeatMode>>>();
  });
});
