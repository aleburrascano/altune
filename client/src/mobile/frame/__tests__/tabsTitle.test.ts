import { pageTitleFor } from '@shared/ui';

import { tabsTitleFor } from '@/mobile';

const PLAYLISTS = [
  { id: 'p1', name: 'Road Trip' },
  { id: 'p2', name: 'Focus' },
];

describe('tabsTitleFor', () => {
  it('titles a tab page by its pathname', () => {
    expect(tabsTitleFor('/discover', PLAYLISTS)).toBe(pageTitleFor('/discover'));
  });

  it('puts the playlist name in the title on a playlist page', () => {
    expect(tabsTitleFor('/library/playlist/p1', PLAYLISTS)).toBe(
      pageTitleFor('/library/playlist/p1', 'Road Trip'),
    );
  });

  it('falls back to the plain title when the playlist is not in the list', () => {
    expect(tabsTitleFor('/library/playlist/missing', PLAYLISTS)).toBe(
      pageTitleFor('/library/playlist/missing', undefined),
    );
  });

  it('ignores a playlist page path with no id', () => {
    expect(tabsTitleFor('/library/playlist/', PLAYLISTS)).toBe(
      pageTitleFor('/library/playlist/', undefined),
    );
  });
});
