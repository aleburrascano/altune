import React from 'react';
import { render, screen } from '@testing-library/react-native';

import { BlendedSection, SECTION_ITEM_CAP } from '../ui/BlendedSection';
import { resultFixture } from './fixtures';

import type { DiscoveryResult, ResultSection } from '@shared/api-client/discovery';
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

function trackItems(count: number): DiscoveryResult[] {
  return Array.from({ length: count }, (_, i) =>
    resultFixture({
      kind: 'track',
      title: `Track ${i}`,
      sources: [{ provider: 'spotify', external_id: `ext-${i}`, url: 'https://x' }],
    }),
  );
}

function trackSection(itemCount: number, hasMore = false): ResultSection {
  return { kind: 'track', items: trackItems(itemCount), has_more: hasMore };
}

function renderBlended(section: ResultSection): void {
  render(
    <BlendedSection
      sections={[section]}
      topResult={undefined}
      onSeeAll={jest.fn()}
      common={commonProps()}
    />,
  );
}

describe('a blended section renders a bounded number of rows whatever the server sends', () => {
  it('renders every item of a section that stays within the cap', () => {
    renderBlended(trackSection(SECTION_ITEM_CAP - 1));

    expect(screen.queryAllByTestId(/^discover-row-track-/)).toHaveLength(SECTION_ITEM_CAP - 1);
  });

  it('renders only the first SECTION_ITEM_CAP rows of an oversized section', () => {
    renderBlended(trackSection(SECTION_ITEM_CAP * 10));

    expect(screen.queryAllByTestId(/^discover-row-track-/)).toHaveLength(SECTION_ITEM_CAP);
    expect(screen.getByText(`Track ${SECTION_ITEM_CAP - 1}`)).toBeTruthy();
    expect(screen.queryByText(`Track ${SECTION_ITEM_CAP}`)).toBeNull();
  });

  it('offers see-all on a truncated section so the dropped items stay reachable', () => {
    renderBlended(trackSection(SECTION_ITEM_CAP * 10));

    expect(screen.getByTestId('discover-see-all-track')).toBeTruthy();
  });

  it('offers no see-all on a section the server called complete and the cap left whole', () => {
    renderBlended(trackSection(SECTION_ITEM_CAP));

    expect(screen.queryByTestId('discover-see-all-track')).toBeNull();
  });
});
