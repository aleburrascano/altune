import { asyncViewForDiscoverView, type DiscoverView } from '../state';
import type { AsyncView } from '@shared/lib/async-view';

describe('asyncViewForDiscoverView', () => {
  const cases: [DiscoverView, AsyncView][] = [
    ['loading', 'loading'],
    ['full-error', 'error'],
    ['empty-no-query', 'empty'],
    ['results', 'ready'],
    ['zero-results', 'ready'],
    ['unavailable', 'ready'],
  ];

  it.each(cases)('maps %s to %s', (view, expected) => {
    expect(asyncViewForDiscoverView(view)).toBe(expected);
  });
});
