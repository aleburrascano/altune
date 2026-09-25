import React from 'react';
import { StyleSheet, type TextStyle } from 'react-native';
import { render, screen } from '@testing-library/react-native';

import { darkTheme, spacing } from '@shared/ui/theme';

import { BlendedSection } from '../ui/BlendedSection';
import { DiscoverBody } from '../ui/DiscoverBody';
import { TopResultCard } from '../ui/TopResultCard';
import { resultFixture } from './fixtures';

import type { ResultsCommonProps } from '../ui/ResultsList';

jest.mock('../hooks/usePreviewPlayback', () => ({
  usePreviewPlayback: () => ({ hasPreview: false }),
}));

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
      view="empty-no-query"
      searchData={undefined}
      historyItems={[
        { query: 'radiohead', query_norm: 'radiohead', executed_at: '2026-01-01T00:00:00.000Z' },
      ]}
      filter="all"
      onFilterChange={jest.fn()}
      onHistoryTap={jest.fn()}
      onClearHistory={jest.fn()}
      onResultTap={jest.fn()}
      impression={commonProps().impression}
      onRetry={jest.fn()}
      onEndReached={jest.fn()}
      isFetchingNextPage={false}
      onRefresh={jest.fn()}
      isRefreshing={false}
      correction={null}
      onSearchOriginal={jest.fn()}
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
