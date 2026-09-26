import React from 'react';
import { Platform } from 'react-native';
import { render, screen } from '@testing-library/react-native';

import { DiscoverBody } from '../ui/DiscoverBody';
import { resultFixture } from './fixtures';

import type { DiscoverView } from '../state';
import { fireEvent } from '@testing-library/react-native';
import type { ResultSection } from '@shared/api-client/discovery';

const NATIVE_OS = Platform.OS;

// The wide Discover layout only ever applies on web (see useWideWebLayout);
// these describe blocks exercise that layout and so need Platform.OS = 'web'
// for the duration of their tests, restored immediately after each one.
function useWebPlatform() {
  beforeEach(() => {
    Platform.OS = 'web';
  });

  afterEach(() => {
    Platform.OS = NATIVE_OS;
  });
}

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
  useWebPlatform();
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
  useWebPlatform();
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
  useWebPlatform();
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

type ProbeResult = ReturnType<typeof resultFixture>;

function probeItem(kind: 'track' | 'album' | 'artist', index: number): ProbeResult {
  const label = kind === 'track' ? 'Track' : kind === 'album' ? 'Album' : 'Artist';
  return resultFixture({
    kind,
    title: `${label} ${index}`,
    sources: [{ provider: 'spotify', external_id: `${kind}-${index}`, url: 'https://x' }],
  });
}

function probeSection(kind: 'track' | 'album' | 'artist', count: number, hasMore = false): ResultSection {
  return { kind, items: Array.from({ length: count }, (_, i) => probeItem(kind, i)), has_more: hasMore };
}

function probeStandardSections(): ResultSection[] {
  return [probeSection('track', 2), probeSection('album', 3), probeSection('artist', 2)];
}

function probeBody(
  sections: ResultSection[],
  topResult: ProbeResult | undefined,
  onResultTap: (result: ProbeResult, position: number) => void = jest.fn(),
  filter: React.ComponentProps<typeof DiscoverBody>['filter'] = 'all',
) {
  const results = topResult ? [topResult] : sections.flatMap((section) => section.items);
  return (
    <DiscoverBody
      view="results"
      searchData={{ results, sections, top_result: topResult }}
      historyItems={[]}
      filter={filter}
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
    />
  );
}

describe('the wide layout starts at the shared 1000px breakpoint and follows a resize', () => {
  useWebPlatform();
  afterEach(() => {
    mockWindowWidth = 390;
  });

  it('pairs the top result and grids albums and artists at exactly 1000px', () => {
    mockWindowWidth = 1000;
    render(probeBody(probeStandardSections(), probeItem('track', 9)));

    expect(screen.getByTestId('discover-top-pair')).toBeTruthy();
    expect(screen.getByTestId('discover-grid-album')).toBeTruthy();
    expect(screen.getByTestId('discover-grid-artist')).toBeTruthy();
    expect(screen.queryByTestId('discover-row-album-0')).toBeNull();
  });

  it('uses between four and six grid columns at exactly 1000px', () => {
    mockWindowWidth = 1000;
    render(probeBody(probeStandardSections(), probeItem('track', 9)));

    const bases = flatStyle(screen.getByTestId('discover-grid-card-album-0').props.style)
      .map((entry) => entry?.flexBasis)
      .filter((basis) => basis !== undefined);
    expect(['25%', '20%', `${100 / 6}%`]).toContain(bases[bases.length - 1]);
  });

  it('keeps album and artist rows and no pairing at 999px', () => {
    mockWindowWidth = 999;
    render(probeBody(probeStandardSections(), probeItem('track', 9)));

    expect(screen.queryByTestId('discover-top-pair')).toBeNull();
    expect(screen.queryByTestId('discover-grid-album')).toBeNull();
    expect(screen.queryByTestId('discover-grid-artist')).toBeNull();
    expect(screen.getByTestId('discover-row-album-0')).toBeTruthy();
    expect(screen.getByTestId('discover-row-artist-0')).toBeTruthy();
  });

  it('keeps album and artist rows and a stacked top result at 360px', () => {
    mockWindowWidth = 360;
    render(probeBody(probeStandardSections(), probeItem('track', 9)));

    expect(screen.queryByTestId('discover-top-pair')).toBeNull();
    expect(screen.getByTestId('discover-top-result')).toBeTruthy();
    expect(screen.queryByTestId(/^discover-grid-/)).toBeNull();
    expect(screen.getByTestId('discover-row-album-2')).toBeTruthy();
    expect(screen.getByTestId('discover-row-artist-1')).toBeTruthy();
  });

  it('switches to grids when the window widens past the breakpoint and back to rows when it narrows', () => {
    mockWindowWidth = 999;
    const { rerender } = render(probeBody(probeStandardSections(), probeItem('track', 9)));
    expect(screen.queryByTestId('discover-grid-album')).toBeNull();

    mockWindowWidth = 1000;
    rerender(probeBody(probeStandardSections(), probeItem('track', 9)));
    expect(screen.getByTestId('discover-grid-album')).toBeTruthy();
    expect(screen.getByTestId('discover-top-pair')).toBeTruthy();
    expect(screen.queryByTestId('discover-row-album-0')).toBeNull();

    mockWindowWidth = 999;
    rerender(probeBody(probeStandardSections(), probeItem('track', 9)));
    expect(screen.queryByTestId('discover-grid-album')).toBeNull();
    expect(screen.queryByTestId('discover-top-pair')).toBeNull();
    expect(screen.getByTestId('discover-row-album-0')).toBeTruthy();
    expect(screen.getAllByTestId('discover-top-result')).toHaveLength(1);
  });
});

describe('the wide top result survives a search with no tracks', () => {
  useWebPlatform();
  afterEach(() => {
    mockWindowWidth = 390;
  });

  it('still shows the top result when the search has no sections at 1440px', () => {
    mockWindowWidth = 1440;
    render(probeBody([], probeItem('track', 9)));

    expect(screen.getByTestId('discover-top-result')).toBeTruthy();
    expect(screen.getByText('Track 9')).toBeTruthy();
    expect(screen.queryByTestId('discover-top-pair')).toBeNull();
  });

  it('shows the top result once and no track rows when the tracks section is empty at 1440px', () => {
    mockWindowWidth = 1440;
    render(probeBody([probeSection('track', 0), probeSection('album', 2)], probeItem('album', 9)));

    expect(screen.getAllByTestId('discover-top-result')).toHaveLength(1);
    expect(screen.getByTestId('discover-grid-album')).toBeTruthy();
    expect(screen.queryByTestId(/^discover-row-track-/)).toBeNull();
  });

  it('shows the top result and every track of the paired section exactly once at 1440px', () => {
    mockWindowWidth = 1440;
    render(probeBody([probeSection('track', 3), probeSection('album', 1)], probeItem('track', 9)));

    expect(screen.getByTestId('discover-top-pair')).toBeTruthy();
    expect(screen.getAllByText('Track 0')).toHaveLength(1);
    expect(screen.getAllByText('Track 1')).toHaveLength(1);
    expect(screen.getAllByText('Track 2')).toHaveLength(1);
    expect(screen.getAllByText('Track 9')).toHaveLength(1);
  });
});

describe('wide grids hold one card, cap many, and give way to filters', () => {
  useWebPlatform();
  afterEach(() => {
    mockWindowWidth = 390;
  });

  it('grids a single album as one card', () => {
    mockWindowWidth = 1440;
    render(probeBody([probeSection('track', 1), probeSection('album', 1)], probeItem('track', 9)));

    expect(screen.getAllByTestId(/^discover-grid-card-album-\d+$/)).toHaveLength(1);
  });

  it('keeps the twenty-item section cap and see-all in a wide grid', () => {
    mockWindowWidth = 1440;
    render(probeBody([probeSection('track', 1), probeSection('album', 45, true)], probeItem('track', 9)));

    expect(screen.getAllByTestId(/^discover-grid-card-album-\d+$/)).toHaveLength(20);
    expect(screen.getByText('Album 19')).toBeTruthy();
    expect(screen.queryByText('Album 20')).toBeNull();
    expect(screen.getByTestId('discover-see-all-album')).toBeTruthy();
  });

  it('drops an empty artist section while gridding the albums after it', () => {
    mockWindowWidth = 1440;
    render(
      probeBody([probeSection('track', 1), probeSection('artist', 0), probeSection('album', 3)], probeItem('track', 9)),
    );

    expect(screen.queryByTestId('discover-grid-artist')).toBeNull();
    expect(screen.queryByText('ARTISTS')).toBeNull();
    expect(screen.getAllByTestId(/^discover-grid-card-album-\d+$/)).toHaveLength(3);
  });

  it('shows the filtered empty view, not a grid, when the album filter matches nothing at 1440px', () => {
    mockWindowWidth = 1440;
    render(probeBody([probeSection('track', 2)], probeItem('track', 9), jest.fn(), 'album'));

    expect(screen.getByTestId('discover-filtered-empty')).toBeTruthy();
    expect(screen.queryByTestId(/^discover-grid-/)).toBeNull();
  });
});

describe('wide cards keep focus and hover visible and still open on press', () => {
  useWebPlatform();
  afterEach(() => {
    mockWindowWidth = 390;
  });

  it('keeps the focus ring on a grid card after the pointer leaves it', () => {
    mockWindowWidth = 1440;
    render(probeBody(probeStandardSections(), probeItem('track', 9)));
    const card = screen.getByTestId('discover-grid-card-album-1');

    fireEvent(card, 'focus');
    fireEvent(card, 'hoverIn');
    fireEvent(card, 'hoverOut');
    expect(gridCardBorderColor('album', 1)).not.toBe('transparent');

    fireEvent(card, 'blur');
    expect(gridCardBorderColor('album', 1)).toBe('transparent');
  });

  it('keeps the hover ring on the top result after focus leaves it while still hovered', () => {
    mockWindowWidth = 1440;
    render(probeBody(probeStandardSections(), probeItem('track', 9)));
    const card = screen.getByTestId('discover-top-result');

    fireEvent(card, 'hoverIn');
    fireEvent(card, 'focus');
    fireEvent(card, 'blur');
    expect(cardBorderColor()).not.toBe('transparent');
  });

  it('rings only the grid card under the pointer', () => {
    mockWindowWidth = 1440;
    render(probeBody(probeStandardSections(), probeItem('track', 9)));

    fireEvent(screen.getByTestId('discover-grid-card-album-2'), 'hoverIn');
    expect(gridCardBorderColor('album', 2)).not.toBe('transparent');
    expect(gridCardBorderColor('album', 0)).toBe('transparent');
    expect(gridCardBorderColor('artist', 0)).toBe('transparent');
  });

  it('exposes grid cards and the top result as buttons, so Enter can activate them', () => {
    mockWindowWidth = 1440;
    render(probeBody(probeStandardSections(), probeItem('track', 9)));

    const role = (testID: string) => {
      const props = screen.getByTestId(testID).props;
      return props.accessibilityRole ?? props.role;
    };
    expect(role('discover-grid-card-album-1')).toBe('button');
    expect(role('discover-grid-card-artist-0')).toBe('button');
    expect(role('discover-top-result')).toBe('button');
  });

  it('opens the top result when pressed in the wide pair', () => {
    mockWindowWidth = 1440;
    const onResultTap = jest.fn();
    render(probeBody(probeStandardSections(), probeItem('track', 9), onResultTap));

    fireEvent.press(screen.getByTestId('discover-top-result'));
    expect(onResultTap).toHaveBeenCalledTimes(1);
    expect(onResultTap).toHaveBeenCalledWith(expect.objectContaining({ title: 'Track 9' }), expect.any(Number));
  });

  it('reports the same result and position for a press at 1440px as for the same item at 390px', () => {
    const narrowTap = jest.fn();
    mockWindowWidth = 390;
    const { unmount } = render(probeBody(probeStandardSections(), probeItem('track', 9), narrowTap));
    fireEvent.press(screen.getByTestId('discover-row-album-2'));
    fireEvent.press(screen.getByTestId('discover-row-artist-1'));
    fireEvent.press(screen.getByTestId('discover-top-result'));
    fireEvent.press(screen.getByTestId('discover-row-track-1'));
    unmount();

    const wideTap = jest.fn();
    mockWindowWidth = 1440;
    render(probeBody(probeStandardSections(), probeItem('track', 9), wideTap));
    fireEvent.press(screen.getByTestId('discover-grid-card-album-2'));
    fireEvent.press(screen.getByTestId('discover-grid-card-artist-1'));
    fireEvent.press(screen.getByTestId('discover-top-result'));
    fireEvent.press(screen.getByTestId('discover-row-track-1'));

    expect(narrowTap).toHaveBeenCalledTimes(4);
    expect(wideTap.mock.calls).toEqual(narrowTap.mock.calls);
  });
});

describe('the wide top pair needs a top result to pair with', () => {
  useWebPlatform();
  afterEach(() => {
    mockWindowWidth = 390;
  });

  it('renders the tracks and no pair when there is no top result at 1440px', () => {
    mockWindowWidth = 1440;
    render(probeBody([probeSection('track', 2), probeSection('album', 1)], undefined));

    expect(screen.queryByTestId('discover-top-pair')).toBeNull();
    expect(screen.queryByTestId('discover-top-result')).toBeNull();
    expect(screen.getByTestId('discover-row-track-0')).toBeTruthy();
    expect(screen.getByTestId('discover-row-track-1')).toBeTruthy();
    expect(screen.getByTestId('discover-grid-album')).toBeTruthy();
  });
});

describe('wide Discover is web only', () => {
  afterEach(() => {
    mockWindowWidth = 390;
  });

  it('keeps rows and no pairing at 1440px on native', () => {
    mockWindowWidth = 1440;
    render(probeBody(probeStandardSections(), probeItem('track', 9)));

    expect(screen.queryByTestId('discover-top-pair')).toBeNull();
    expect(screen.queryByTestId('discover-grid-album')).toBeNull();
    expect(screen.queryByTestId('discover-grid-artist')).toBeNull();
    expect(screen.getByTestId('discover-row-album-0')).toBeTruthy();
    expect(screen.getByTestId('discover-row-artist-0')).toBeTruthy();
    expect(screen.getByTestId('discover-top-result')).toBeTruthy();
  });
});
