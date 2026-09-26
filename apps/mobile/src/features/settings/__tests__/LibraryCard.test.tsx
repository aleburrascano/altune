import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, within } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { backfillFeaturedArtists } from '@shared/api-client/tracks';
import { LibraryCard } from '../ui/LibraryCard';

jest.mock('@shared/api-client/tracks', () => ({
  ...jest.requireActual('@shared/api-client/tracks'),
  backfillFeaturedArtists: jest.fn(),
}));

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

const backfillRow = () => within(screen.getByTestId('settings-backfill-featured'));

beforeEach(() => {
  jest.mocked(backfillFeaturedArtists).mockReset();
});

describe('LibraryCard', () => {
  it('renders the backfill row idle with the Run label', () => {
    render(<LibraryCard />, { wrapper });
    expect(backfillRow().getByText('Resolve featured artists')).toBeTruthy();
    expect(backfillRow().getByText('Run')).toBeTruthy();
  });

  it('presses the row and reports Done with the counts on success', async () => {
    jest.mocked(backfillFeaturedArtists).mockResolvedValue({ updated: 2, scanned: 9 });
    render(<LibraryCard />, { wrapper });

    fireEvent.press(screen.getByTestId('settings-backfill-featured'));

    expect(await backfillRow().findByText('Done')).toBeTruthy();
    expect(backfillRow().getByText('Updated 2 of 9 tracks')).toBeTruthy();
  });
});
