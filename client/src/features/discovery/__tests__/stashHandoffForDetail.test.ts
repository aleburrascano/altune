import { readDetailHandoff } from '@shared/navigation/detailHandoff';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';

import { stashHandoffForDetail } from '@features/discovery/domain/stashHandoffForDetail';
import { resultFixture } from '@features/discovery/__tests__/fixtures';

beforeEach(() => {
  runSignOutCleanups();
});

describe('stashHandoffForDetail is the discover to detail navigation seam', () => {
  it('returns an href to the discover detail route', () => {
    expect(stashHandoffForDetail(resultFixture()).pathname).toBe('/discover/detail');
  });

  it('carries the tapped result so the detail screen can read it back', () => {
    const result = resultFixture({ title: 'Paranoid Android' });

    const href = stashHandoffForDetail(result, 'search-42');

    const handoff = readDetailHandoff(href.params.handoff);
    expect(handoff?.result).toBe(result);
    expect(handoff?.searchId).toBe('search-42');
  });

  it('stores a null search id when none is supplied', () => {
    const href = stashHandoffForDetail(resultFixture());

    expect(readDetailHandoff(href.params.handoff)?.searchId).toBeNull();
  });
});
