import React from 'react';
import { render as renderBare, screen } from '@testing-library/react-native';

import { FilteredResults } from '@features/discovery/ui/FilteredResults';
import { resultFixture } from '@features/discovery/__tests__/fixtures';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { SearchCorrection } from '@features/discovery/domain/discoverView';
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
