import React from 'react';
import { render, screen } from '@testing-library/react-native';

import { DiscoverBody } from '../ui/DiscoverBody';
import { resultFixture } from './fixtures';

import type { DiscoverView } from '../state';
import { fireEvent } from '@testing-library/react-native';
import type { ResultSection } from '@shared/api-client/discovery';

jest.mock('../hooks/usePreviewPlayback', () => ({
  usePreviewPlayback: () => ({ hasPreview: false }),
}));

let mockWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 900, scale: 2, fontScale: 1 }),
}));

function renderBody(view: DiscoverView, resultsIncomplete: boolean) {
  const results = view === 'results' ? [resultFixture()] : [];
  render(
    <DiscoverBody
      view={view}
      // Rows need a PlaybackProvider; the banner does not depend on them, so no sections.
      searchData={{ results, sections: [] }}
      resultsIncomplete={resultsIncomplete}
      historyItems={[]}
      filter="all"
      onFilterChange={jest.fn()}
      onHistoryTap={jest.fn()}
      onClearHistory={jest.fn()}
      onResultTap={jest.fn()}
      impression={{
        viewabilityConfig: { itemVisiblePercentThreshold: 50 },
        onViewableItemsChanged: jest.fn(),
      }}
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

describe('DiscoverBody flags a partial (degraded) search as possibly incomplete', () => {
  it('shows the incomplete-results banner over partial results', () => {
    renderBody('results', true);

    expect(screen.getByTestId('discover-results')).toBeTruthy();
    expect(screen.getByTestId('discover-incomplete-results')).toBeTruthy();
    expect(screen.getByText(/Some results may be missing/)).toBeTruthy();
  });

  it('renders no banner for the same results from a healthy search', () => {
    renderBody('results', false);

    expect(screen.getByTestId('discover-results')).toBeTruthy();
    expect(screen.queryByTestId('discover-incomplete-results')).toBeNull();
  });

  it('shows the banner on a partial zero-results search, so "No matches" is not taken as final', () => {
    renderBody('zero-results', true);

    expect(screen.getByTestId('discover-zero-results')).toBeTruthy();
    expect(screen.getByTestId('discover-incomplete-results')).toBeTruthy();
  });
});

describe('clear history failure', () => {
  function renderEmpty(clearHistoryFailed: boolean) {
    render(
      <DiscoverBody
        view="empty-no-query"
        searchData={undefined}
        historyItems={[{ query: 'old', query_norm: 'old' } as never]}
        filter="all"
        onFilterChange={jest.fn()}
        onHistoryTap={jest.fn()}
        onResultTap={jest.fn()}
        impression={{
          viewabilityConfig: { itemVisiblePercentThreshold: 50 },
          onViewableItemsChanged: jest.fn(),
        }}
        onRetry={jest.fn()}
        onEndReached={jest.fn()}
        isFetchingNextPage={false}
        onRefresh={jest.fn()}
        isRefreshing={false}
        correction={null}
        onSearchOriginal={jest.fn()}
        onClearHistory={jest.fn()}
        clearHistoryFailed={clearHistoryFailed}
      />,
    );
  }

  it('shows a message when the clear failed', () => {
    renderEmpty(true);
    expect(screen.getByTestId('discover-clear-history-error')).toBeTruthy();
  });

  it('shows no message otherwise', () => {
    renderEmpty(false);
    expect(screen.queryByTestId('discover-clear-history-error')).toBeNull();
  });
});

function albumResult(index: number) {
  return resultFixture({
    kind: 'album',
    title: `Album ${index}`,
    sources: [{ provider: 'spotify', external_id: `album-${index}`, url: 'https://x' }],
  });
}

function artistResult(index: number) {
  return resultFixture({
    kind: 'artist',
    title: `Artist ${index}`,
    sources: [{ provider: 'spotify', external_id: `artist-${index}`, url: 'https://x' }],
  });
}

function blendedSections(): ResultSection[] {
  return [
    { kind: 'track', items: [resultFixture({ kind: 'track', title: 'Track 0' })], has_more: false },
    { kind: 'album', items: [albumResult(0), albumResult(1)], has_more: false },
  ];
}

function sectionsWithArtistAndEmptySection(): ResultSection[] {
  return [
    { kind: 'track', items: [resultFixture({ kind: 'track', title: 'Track 0' })], has_more: false },
    { kind: 'artist', items: [artistResult(0)], has_more: false },
    { kind: 'album', items: [], has_more: false },
  ];
}

function noTrackFirstSections(): ResultSection[] {
  return [
    { kind: 'album', items: [albumResult(0)], has_more: false },
  ];
}

function renderSearchBody(sections: ResultSection[], topResult: ReturnType<typeof resultFixture>) {
  render(
    <DiscoverBody
      view="results"
      searchData={{ results: [topResult], sections, top_result: topResult }}
      historyItems={[]}
      filter="all"
      onFilterChange={jest.fn()}
      onHistoryTap={jest.fn()}
      onClearHistory={jest.fn()}
      onResultTap={jest.fn()}
      impression={{
        viewabilityConfig: { itemVisiblePercentThreshold: 50 },
        onViewableItemsChanged: jest.fn(),
      }}
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

function renderBlendedBody() {
  const topResult = resultFixture({ kind: 'track', title: 'Top' });
  renderSearchBody(blendedSections(), topResult);
}

function renderArtistGridBody() {
  const topResult = resultFixture({ kind: 'track', title: 'Top' });
  renderSearchBody(sectionsWithArtistAndEmptySection(), topResult);
}

function renderNoTrackFirstBody() {
  const topResult = resultFixture({ kind: 'album', title: 'Top Album' });
  renderSearchBody(noTrackFirstSections(), topResult);
}

function flatStyle(style: unknown): Record<string, unknown>[] {
  return ([] as unknown[]).concat(style).flat(Infinity) as Record<string, unknown>[];
}

function lastMatchingBorderColor(style: unknown): unknown {
  const entries = flatStyle(style).filter((entry) => entry?.borderColor !== undefined);
  return entries.length > 0 ? entries[entries.length - 1]?.borderColor : undefined;
}

function cardBorderColor(): unknown {
  return lastMatchingBorderColor(screen.getByTestId('discover-top-result-card').props.style);
}

function gridCardBorderColor(kind: string, position: number): unknown {
  return lastMatchingBorderColor(screen.getByTestId(`discover-grid-card-body-${kind}-${position}`).props.style);
}

describe('wide layout pairs the top result with tracks and grids albums', () => {
  afterEach(() => {
    mockWindowWidth = 390;
  });

  it('pairs the top result beside the first tracks section and grids albums at 1440px', () => {
    mockWindowWidth = 1440;
    renderBlendedBody();

    expect(screen.getByTestId('discover-top-pair')).toBeTruthy();
    expect(screen.getByTestId('discover-grid-album')).toBeTruthy();
    expect(screen.queryByTestId('discover-row-album-0')).toBeNull();
  });

  it('stacks the top result above sections and keeps album rows on a compact screen', () => {
    mockWindowWidth = 390;
    renderBlendedBody();

    expect(screen.queryByTestId('discover-top-pair')).toBeNull();
    expect(screen.queryByTestId('discover-grid-album')).toBeNull();
    expect(screen.getByTestId('discover-row-album-0')).toBeTruthy();
  });

  it('rings the top result card on keyboard focus', () => {
    mockWindowWidth = 1440;
    renderBlendedBody();
    expect(cardBorderColor()).toBe('transparent');

    fireEvent(screen.getByTestId('discover-top-result'), 'focus');
    expect(cardBorderColor()).not.toBe('transparent');
  });

  it('rings the top result card on hover, and unrings it on hover-out and blur', () => {
    mockWindowWidth = 1440;
    renderBlendedBody();
    expect(cardBorderColor()).toBe('transparent');

    fireEvent(screen.getByTestId('discover-top-result'), 'hoverIn');
    expect(cardBorderColor()).not.toBe('transparent');
    fireEvent(screen.getByTestId('discover-top-result'), 'hoverOut');
    expect(cardBorderColor()).toBe('transparent');

    fireEvent(screen.getByTestId('discover-top-result'), 'focus');
    expect(cardBorderColor()).not.toBe('transparent');
    fireEvent(screen.getByTestId('discover-top-result'), 'blur');
    expect(cardBorderColor()).toBe('transparent');
  });
});

describe('wide layout grids every eligible kind and drops empty sections', () => {
  afterEach(() => {
    mockWindowWidth = 390;
  });

  it('grids an artist section and renders no section for an empty one, at 1440px', () => {
    mockWindowWidth = 1440;
    renderArtistGridBody();

    expect(screen.getByTestId('discover-grid-artist')).toBeTruthy();
    expect(screen.queryByTestId('discover-grid-album')).toBeNull();
    expect(screen.queryByTestId('discover-row-album-0')).toBeNull();
    expect(screen.queryByText('ALBUMS')).toBeNull();
  });

  it('sizes grid cards for five columns between 1280 and 1600px', () => {
    mockWindowWidth = 1440;
    renderArtistGridBody();

    const card = screen.getByTestId('discover-grid-card-artist-0');
    expect(flatStyle(card.props.style).some((entry) => entry?.flexBasis === '20%')).toBe(true);
  });

  it('uses six columns at 1600px and four columns just under 1280px', () => {
    mockWindowWidth = 1600;
    renderArtistGridBody();
    expect(
      flatStyle(screen.getByTestId('discover-grid-card-artist-0').props.style).some(
        (entry) => entry?.flexBasis === `${100 / 6}%`,
      ),
    ).toBe(true);
  });

  it('uses four columns just under 1280px', () => {
    mockWindowWidth = 1279;
    renderArtistGridBody();
    expect(
      flatStyle(screen.getByTestId('discover-grid-card-artist-0').props.style).some(
        (entry) => entry?.flexBasis === '25%',
      ),
    ).toBe(true);
  });

  it('rings a grid card on hover, and unrings it on hover-out, focus, and blur', () => {
    mockWindowWidth = 1440;
    renderArtistGridBody();
    const card = () => screen.getByTestId('discover-grid-card-artist-0');
    expect(gridCardBorderColor('artist', 0)).toBe('transparent');

    fireEvent(card(), 'hoverIn');
    expect(gridCardBorderColor('artist', 0)).not.toBe('transparent');
    fireEvent(card(), 'hoverOut');
    expect(gridCardBorderColor('artist', 0)).toBe('transparent');

    fireEvent(card(), 'focus');
    expect(gridCardBorderColor('artist', 0)).not.toBe('transparent');
    fireEvent(card(), 'blur');
    expect(gridCardBorderColor('artist', 0)).toBe('transparent');
  });

  it('does not pair the top result when the first section is not tracks', () => {
    mockWindowWidth = 1440;
    renderNoTrackFirstBody();

    expect(screen.queryByTestId('discover-top-pair')).toBeNull();
    expect(screen.getByTestId('discover-top-result')).toBeTruthy();
  });
});

describe('wide layout grid column exactness and card taps', () => {
  afterEach(() => {
    mockWindowWidth = 390;
  });

  it('uses five columns at exactly 1280px', () => {
    mockWindowWidth = 1280;
    renderArtistGridBody();
    expect(
      flatStyle(screen.getByTestId('discover-grid-card-artist-0').props.style).some(
        (entry) => entry?.flexBasis === '20%',
      ),
    ).toBe(true);
  });

  it('renders artwork for a grid card and taps it through to onResultTap', () => {
    mockWindowWidth = 1440;
    const onResultTap = jest.fn();
    const topResult = resultFixture({ kind: 'track', title: 'Top' });
    render(
      <DiscoverBody
        view="results"
        searchData={{ results: [topResult], sections: sectionsWithArtistAndEmptySection(), top_result: topResult }}
        historyItems={[]}
        filter="all"
        onFilterChange={jest.fn()}
        onHistoryTap={jest.fn()}
        onClearHistory={jest.fn()}
        onResultTap={onResultTap}
        impression={{
          viewabilityConfig: { itemVisiblePercentThreshold: 50 },
          onViewableItemsChanged: jest.fn(),
        }}
        onRetry={jest.fn()}
        onEndReached={jest.fn()}
        isFetchingNextPage={false}
        onRefresh={jest.fn()}
        isRefreshing={false}
        correction={null}
        onSearchOriginal={jest.fn()}
      />,
    );

    const artistArtwork = screen.getAllByTestId('artwork').find((node) => node.props.accessibilityLabel === 'Artist 0');
    expect(artistArtwork).toBeTruthy();

    fireEvent.press(screen.getByTestId('discover-grid-card-artist-0'));
    expect(onResultTap).toHaveBeenCalledWith(expect.objectContaining({ title: 'Artist 0' }), 0);
  });
});
