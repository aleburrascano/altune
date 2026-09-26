import { pageTitleFor } from '../pageTitle';

describe('pageTitleFor', () => {
  it('titles the Library tab', () => {
    expect(pageTitleFor('/library')).toBe('Library · Altune');
  });

  it('titles the Discover tab', () => {
    expect(pageTitleFor('/discover')).toBe('Discover · Altune');
  });

  it('titles the Settings tab', () => {
    expect(pageTitleFor('/settings')).toBe('Settings · Altune');
  });

  it('titles a playlist page with its name once known', () => {
    expect(pageTitleFor('/library/playlist/abc123', 'Road Trip')).toBe('Road Trip · Altune');
  });

  it('falls back to a generic title on a playlist page before the name loads', () => {
    expect(pageTitleFor('/library/playlist/abc123', undefined)).toBe('Playlist · Altune');
    expect(pageTitleFor('/library/playlist/abc123', undefined)).not.toMatch(/undefined/);
  });

  it('falls back to the app name on an unknown route', () => {
    expect(pageTitleFor('/some/other/route')).toBe('Altune');
  });
});
