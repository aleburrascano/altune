import {
  clearDetailHandoff,
  getDetailHandoff,
  getDetailHandoffSearchId,
} from '@shared/lib/detail-handoff';

import { stashHandoffForDetail } from '../tap';
import { resultFixture } from './fixtures';

beforeEach(() => {
  clearDetailHandoff();
});

describe('stashHandoffForDetail is the discover to detail navigation seam', () => {
  it('returns the detail route path', () => {
    expect(stashHandoffForDetail(resultFixture())).toBe('/discover/detail');
  });

  it('stashes the tapped result so the detail screen can read it back', () => {
    const result = resultFixture({ title: 'Paranoid Android' });

    stashHandoffForDetail(result, 'search-42');

    expect(getDetailHandoff()).toBe(result);
    expect(getDetailHandoffSearchId()).toBe('search-42');
  });

  it('stores a null search id when none is supplied', () => {
    stashHandoffForDetail(resultFixture());

    expect(getDetailHandoffSearchId()).toBeNull();
  });
});
