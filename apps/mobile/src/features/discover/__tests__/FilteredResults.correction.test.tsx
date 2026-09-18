import React from 'react';
import { render, screen } from '@testing-library/react-native';

import { FilteredResults } from '../ui/FilteredResults';
import { resultFixture } from './fixtures';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { SearchCorrection } from '../state';
import type { ResultsCommonProps } from '../ui/ResultsList';

jest.mock('../hooks/usePreviewPlayback', () => ({
  usePreviewPlayback: () => ({ hasPreview: false }),
}));

function commonProps(correction: SearchCorrection | null): ResultsCommonProps {
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
    correction,
    onSearchOriginal: jest.fn(),
  };
}

const misspelled: SearchCorrection = { corrected: 'radiohead', original: 'radiohed' };

function renderFiltered(results: DiscoveryResult[], correction: SearchCorrection | null): void {
  render(<FilteredResults kind="track" results={results} common={commonProps(correction)} />);
}

describe('the correction banner follows the one correction value into both filtered views', () => {
  it('names the corrected and the original query above a filtered result list', () => {
    renderFiltered([resultFixture()], misspelled);

    expect(screen.getByText('radiohead')).toBeTruthy();
    expect(screen.getByLabelText('Search instead for radiohed')).toBeTruthy();
  });

  it('shows no banner above a filtered result list when no correction was applied', () => {
    renderFiltered([resultFixture()], null);

    expect(screen.queryByLabelText('Search instead for radiohed')).toBeNull();
  });

  it('names the corrected and the original query on the empty filtered view', () => {
    renderFiltered([], misspelled);

    expect(screen.getByTestId('discover-filtered-empty')).toBeTruthy();
    expect(screen.getByLabelText('Search instead for radiohed')).toBeTruthy();
  });

  it('shows no banner on the empty filtered view when no correction was applied', () => {
    renderFiltered([], null);

    expect(screen.getByTestId('discover-filtered-empty')).toBeTruthy();
    expect(screen.queryByLabelText('Search instead for radiohed')).toBeNull();
  });
});
