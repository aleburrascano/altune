/**
 * LyricsSection — the Deezer lyrics block (docs/providers/deezer.md cap 6).
 *
 * Pure presentational: synced lines (preferred) or plain text, plus writers and
 * copyright. Renders nothing when there are no lyrics.
 */
import { render } from '@testing-library/react-native';
import { createElement } from 'react';

import { LyricsSection } from '../ui/LyricsSection';
import type { LyricsResponse } from '../../../shared/api-client/discovery';

function _lyrics(over: Partial<LyricsResponse> = {}): LyricsResponse {
  return {
    plain: "Hello, it's me\nI was wondering",
    synced_lines: [
      { timecode: '[00:12.34]', line: "Hello, it's me", milliseconds: 12340, duration: 2000 },
      { timecode: '[00:15.00]', line: 'I was wondering', milliseconds: 15000, duration: 2500 },
    ],
    writers: ['Adele Laurie Blue Adkins', 'Gregory Allen Kurstin'],
    copyright: 'Universal Music',
    ...over,
  };
}

function _render(lyrics: LyricsResponse | null) {
  return render(createElement(LyricsSection, { lyrics }));
}

describe('LyricsSection', () => {
  it('renders synced lines, writers, and copyright', () => {
    const { getByTestId, getByText } = _render(_lyrics());
    expect(getByTestId('detail-lyrics')).toBeTruthy();
    expect(getByText("Hello, it's me")).toBeTruthy();
    expect(getByText('I was wondering')).toBeTruthy();
    expect(getByTestId('detail-lyrics-writers')).toHaveTextContent(
      'Written by Adele Laurie Blue Adkins, Gregory Allen Kurstin',
    );
    expect(getByText('Universal Music')).toBeTruthy();
  });

  it('falls back to plain text when there are no synced lines', () => {
    const { getByText } = _render(_lyrics({ synced_lines: [] }));
    expect(getByText("Hello, it's me")).toBeTruthy();
    expect(getByText('I was wondering')).toBeTruthy();
  });

  it('renders nothing when lyrics are null', () => {
    const { queryByTestId } = _render(null);
    expect(queryByTestId('detail-lyrics')).toBeNull();
  });

  it('renders nothing when the lyrics have no text', () => {
    const { queryByTestId } = _render({
      plain: '',
      synced_lines: [],
      writers: [],
      copyright: '',
    });
    expect(queryByTestId('detail-lyrics')).toBeNull();
  });
});
