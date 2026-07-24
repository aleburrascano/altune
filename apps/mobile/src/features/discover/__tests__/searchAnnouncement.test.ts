import { _searchAnnouncement } from '../ui/DiscoverBody';

describe('_searchAnnouncement', () => {
  it('names the outcome for each settled state', () => {
    expect(_searchAnnouncement('zero-results', 0)).toBe('No matches');
    expect(_searchAnnouncement('full-error', 0)).toBe('Search failed');
    expect(_searchAnnouncement('results', 12)).toBe('12 results');
  });

  it('singularises a lone result', () => {
    expect(_searchAnnouncement('results', 1)).toBe('1 result');
  });

  // Announcing mid-flight would fire on every debounced keystroke.
  it('stays silent while loading or idle', () => {
    expect(_searchAnnouncement('loading', 0)).toBe('');
    expect(_searchAnnouncement('empty-no-query', 0)).toBe('');
  });
});
