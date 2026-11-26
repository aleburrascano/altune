import React from 'react';
import { QueryClient } from '@tanstack/react-query';
import { render, screen, fireEvent, within } from '@testing-library/react-native';

import { signInTestUser } from '@testing/signInTestUser';
import { LibraryCard } from '@features/settings';
import { makeWrapper } from '@testing/makeWrapper';
import { retryOptionsWithJitter } from '@testing/retryOptions';
const { __http } = require('@testing/doubles/fetch.js');

const BACKFILL_ROUTE = 'POST /v1/tracks/featured-backfill';

let wrapper: ReturnType<typeof makeWrapper>;
const retryOptions = retryOptionsWithJitter();

const backfillRow = () => within(screen.getByTestId('settings-backfill-featured'));

beforeEach(async () => {
  wrapper = makeWrapper(
    new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } }),
  );
  await signInTestUser();
});

describe('LibraryCard', () => {
  it('renders the backfill row idle with the Run label', () => {
    render(<LibraryCard retryOptions={retryOptions} />, { wrapper });
    expect(backfillRow().getByText('Resolve featured artists')).toBeTruthy();
    expect(backfillRow().getByText('Run')).toBeTruthy();
  });

  it('presses the row and reports Done with the counts on success', async () => {
    __http.reply(BACKFILL_ROUTE, {
      json: { updated: 2, scanned: 9, truncated: false, next_offset: 9 },
    });
    render(<LibraryCard retryOptions={retryOptions} />, { wrapper });

    fireEvent.press(screen.getByTestId('settings-backfill-featured'));

    expect(await backfillRow().findByText('Done')).toBeTruthy();
    expect(backfillRow().getByText('Updated 2 of 9 tracks')).toBeTruthy();
  });
});
