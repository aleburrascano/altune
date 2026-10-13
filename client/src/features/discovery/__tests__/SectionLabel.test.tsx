import React from 'react';
import { StyleSheet, type TextStyle } from 'react-native';
import { render as renderBare, screen } from '@testing-library/react-native';

import { darkTheme, spacing } from '@shared/ui/theme';

import { BlendedSection } from '@features/discovery/ui/BlendedSection';
import { DiscoverBody } from '@features/discovery/ui/DiscoverBody';
import { TopResultCard } from '@features/discovery/ui/TopResultCard';
import { resultFixture } from '@features/discovery/__tests__/fixtures';

import type { ResultsCommonProps } from '@features/discovery/ui/ResultsList';
import { PlaybackContext } from '@shared/playback/usePlayback';
import { idlePlayback } from '@testing/queryHarness';

function render(ui: React.ReactElement): ReturnType<typeof renderBare> {
  return renderBare(ui, {
    wrapper: ({ children }) => (
      <PlaybackContext.Provider value={idlePlayback}>{children}</PlaybackContext.Provider>
    ),
  });
}

function commonProps(): ResultsCommonProps {
  return {
    onResultTap: jest.fn(),
    impression: {
      viewabilityConfig: { itemVisiblePercentThreshold: 50 },
      onViewableItemsChanged: jest.fn(),
    },
    onRefresh: jest.fn(),
    isRefreshing: false,
    onEndReached: jest.fn(),
    isFetchingNextPage: false,
    correction: null,
    onSearchOriginal: jest.fn(),
  };
}

function styleOfLabel(text: string): TextStyle {
  return StyleSheet.flatten(screen.getByText(text).props.style) ?? {};
}

function renderHistoryBody(): void {
  render(
    <DiscoverBody
      results={{
        view: 'empty-no-query',
        searchData: undefined,
        filter: 'all',
        setFilter: jest.fn(),
        onResultTap: jest.fn(),
        impression: commonProps().impression,
        onRetry: jest.fn(),
        onRefresh: jest.fn(),
        isRefreshing: false,
        correction: null,
        onSearchOriginal: jest.fn(),
      }}
      pagination={{
        onEndReached: jest.fn(),
        isFetchingNextPage: false,
      }}
      history={{
        items: [
          { query: 'radiohead', query_norm: 'radiohead', executed_at: '2026-01-01T00:00:00.000Z' },
        ],
        onTap: jest.fn(),
        onClear: jest.fn(),
      }}
    />,
  );
}

describe('discover section headers share one small-caps idiom', () => {
  it('gives the recent-searches header the shared tracking and tertiary tone', () => {
    renderHistoryBody();

    const style = styleOfLabel('RECENT SEARCHES');

    expect(style.letterSpacing).toBe(1);
    expect(style.color).toBe(darkTheme.color.textTertiary);
    expect(style.fontSize).toBe(14);
  });

  it('leaves the recent-searches header its own margins, which it has none of', () => {
    renderHistoryBody();

    const style = styleOfLabel('RECENT SEARCHES');

    expect(style.marginTop).toBeUndefined();
    expect(style.marginBottom).toBeUndefined();
  });

  it("keeps the top-result header's bottom margin on top of the shared tracking", () => {
    render(<TopResultCard result={resultFixture()} onPress={jest.fn()} />);

    const style = styleOfLabel('TOP RESULT');

    expect(style.letterSpacing).toBe(1);
    expect(style.marginBottom).toBe(spacing.md);
    expect(style.marginTop).toBeUndefined();
  });

  it("keeps a blended section header's margins on top of the shared tracking", () => {
    render(
      <BlendedSection
        sections={[{ kind: 'album', items: [resultFixture({ kind: 'album' })], has_more: false }]}
        topResult={undefined}
        onSeeAll={jest.fn()}
        common={commonProps()}
      />,
    );

    const style = styleOfLabel('ALBUMS');

    expect(style.letterSpacing).toBe(1);
    expect(style.marginTop).toBe(spacing.sm);
    expect(style.marginBottom).toBe(spacing.sm);
  });
});
