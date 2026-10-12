import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, within } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { signInTestUser } from '@testing/signInTestUser';
import { LibraryCard } from '@features/settings/ui/LibraryCard';
import { FeaturePorts } from '@/chrome/FeaturePorts';
import { httpFeaturePorts } from '@/chrome/httpFeaturePorts';

const { __http } = require('@testing/doubles/fetch.js');

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));

const BACKFILL_ROUTE = 'POST /v1/tracks/featured-backfill';

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return (
    <QueryClientProvider client={client}>
      <FeaturePorts ports={httpFeaturePorts}>{children}</FeaturePorts>
    </QueryClientProvider>
  );
}

const backfillRow = () => within(screen.getByTestId('settings-backfill-featured'));

beforeEach(async () => {
  await signInTestUser();
});

describe('LibraryCard', () => {
  it('renders the backfill row idle with the Run label', () => {
    render(<LibraryCard />, { wrapper });
    expect(backfillRow().getByText('Resolve featured artists')).toBeTruthy();
    expect(backfillRow().getByText('Run')).toBeTruthy();
  });

  it('presses the row and reports Done with the counts on success', async () => {
    __http.reply(BACKFILL_ROUTE, {
      json: { updated: 2, scanned: 9, truncated: false, next_offset: 9 },
    });
    render(<LibraryCard />, { wrapper });

    fireEvent.press(screen.getByTestId('settings-backfill-featured'));

    expect(await backfillRow().findByText('Done')).toBeTruthy();
    expect(backfillRow().getByText('Updated 2 of 9 tracks')).toBeTruthy();
  });
});
