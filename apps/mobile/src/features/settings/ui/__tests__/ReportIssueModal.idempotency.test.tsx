// A dropped response leaves the reporter unable to know whether their issue was
// filed, and the failed-state UI answers that by relabelling Send as "Try again".
// The server collapses two submits onto one issue only when both carry the same
// Idempotency-Key, so without a per-draft key the retry files a second issue (#1755).

import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';
import { ReportIssueModal } from '../ReportIssueModal';

const { __http } = require('../../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

let warnSpy: jest.SpyInstance;

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warnSpy.mockRestore();
});

function renderModal(): void {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <ReportIssueModal visible onClose={jest.fn()} screen="settings" />
    </QueryClientProvider>,
  );
}

function submitCount(): number {
  return __http.countFor('POST /v1/feedback/reports');
}

// The server files one issue per distinct Idempotency-Key inside its dedup
// window, and a fresh issue for every submit that carries none — so a keyless
// submit is its own issue, and repeats of one key are a single issue.
function issuesFiled(): number {
  const keys = __http.requests
    .filter((request: { method: string; path: string }) => {
      return request.method === 'POST' && request.path === '/v1/feedback/reports';
    })
    .map((request: { headers: Record<string, string> }, index: number) => {
      return request.headers['Idempotency-Key'] ?? `keyless-${index}`;
    });
  return new Set(keys).size;
}

function writeDraft(message: string): void {
  fireEvent.press(screen.getByTestId('report-issue-kind-bug'));
  fireEvent.changeText(screen.getByTestId('report-issue-message'), message);
}

async function pressSend(expectedSubmits: number): Promise<void> {
  fireEvent.press(screen.getByTestId('report-issue-send'));
  await waitFor(() => expect(submitCount()).toBe(expectedSubmits));
}

describe('a reporter who retries one draft after a dropped response', () => {
  it('files a single issue, however many times the draft is sent', async () => {
    __http.fail('POST /v1/feedback/reports');
    renderModal();
    writeDraft('the queue jumped after a skip');

    await pressSend(1);
    expect(await screen.findByText('Try again')).toBeTruthy();
    await pressSend(2);

    expect(issuesFiled()).toBe(1);
  });

  it('files a distinct issue when the message is edited before the retry', async () => {
    __http.fail('POST /v1/feedback/reports');
    renderModal();
    writeDraft('the queue jumped after a skip');

    await pressSend(1);
    expect(await screen.findByText('Try again')).toBeTruthy();
    fireEvent.changeText(
      screen.getByTestId('report-issue-message'),
      'the queue jumped after a skip, and artwork vanished',
    );
    await pressSend(2);

    expect(issuesFiled()).toBe(2);
  });

  it('files a single issue when only the whitespace around the message changes', async () => {
    __http.fail('POST /v1/feedback/reports');
    renderModal();
    writeDraft('the queue jumped after a skip');

    await pressSend(1);
    expect(await screen.findByText('Try again')).toBeTruthy();
    fireEvent.changeText(
      screen.getByTestId('report-issue-message'),
      '  the queue jumped after a skip \n',
    );
    await pressSend(2);

    expect(issuesFiled()).toBe(1);
  });
});

describe('a reporter who sends a second report after the first was filed', () => {
  it('files a distinct issue, so a new draft cannot be swallowed by the last one', async () => {
    __http.reply('POST /v1/feedback/reports', {
      status: 201,
      json: { issue_number: 42, issue_url: 'https://github.com/x/y/issues/42' },
    });
    renderModal();
    writeDraft('the queue jumped after a skip');
    await pressSend(1);

    fireEvent.press(await screen.findByTestId('report-issue-another'));
    writeDraft('artwork is missing on the album screen');
    await pressSend(2);

    expect(issuesFiled()).toBe(2);
  });
});
