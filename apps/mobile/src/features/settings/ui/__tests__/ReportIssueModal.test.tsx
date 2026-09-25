import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/api-client';
import { submitReport } from '@shared/api-client/feedback';
import { supabase } from '@shared/auth/supabaseClient';
import { ReportIssueModal } from '../ReportIssueModal';

const { __http } = require('../../../../../jest/doubles/fetch.js');

jest.mock('@shared/api-client/feedback', () => ({
  ...jest.requireActual('@shared/api-client/feedback'),
  submitReport: jest.fn(),
}));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const mockSubmitReport = submitReport as jest.Mock;

function renderModal() {
  const onClose = jest.fn();
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <ReportIssueModal visible onClose={onClose} screen="settings" />
    </QueryClientProvider>,
  );
  return { onClose };
}

function sendDisabled(): boolean {
  return screen.getByTestId('report-issue-send').props.accessibilityState.disabled;
}

let warnSpy: jest.SpyInstance;

beforeEach(() => {
  mockSubmitReport.mockReset();
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  warnSpy.mockRestore();
});

describe('ReportIssueModal(): Send is enabled only with a kind and a trimmed message of at least 10 characters', () => {
  it.each([
    ['no kind, long message', null, 'a long enough message', true],
    ['kind, 9 trimmed characters padded with spaces', 'bug', '   123456789   ', true],
    ['kind, exactly 10 characters', 'idea', '1234567890', false],
  ])('%s', (_label, kind, message, expectedDisabled) => {
    renderModal();
    if (kind !== null) fireEvent.press(screen.getByTestId(`report-issue-kind-${kind}`));
    fireEvent.changeText(screen.getByTestId('report-issue-message'), message);

    expect(sendDisabled()).toBe(expectedDisabled);
  });

  it('caps the message field at 2000 characters', () => {
    renderModal();
    expect(screen.getByTestId('report-issue-message').props.maxLength).toBe(2000);
  });
});

describe('ReportIssueModal(): disclosure copy', () => {
  it('tells the reporter the report is tied to their signed-in account', () => {
    renderModal();

    expect(screen.getByText(/sent from your signed-in account/)).toBeTruthy();
    expect(screen.queryByText(/no account needed/i)).toBeNull();
  });
});

describe('ReportIssueModal(): submit flow', () => {
  it('sends the trimmed message, shows the sent screen, and "Send another" returns to an empty form', async () => {
    mockSubmitReport.mockResolvedValue({ issue_number: 42 });
    renderModal();
    fireEvent.press(screen.getByTestId('report-issue-kind-confusing'));
    fireEvent.changeText(screen.getByTestId('report-issue-message'), '  the queue jumped  ');
    fireEvent.press(screen.getByTestId('report-issue-send'));

    expect(await screen.findByText('Sent — thank you')).toBeTruthy();
    expect(screen.getByText('Filed as #42. Every report gets read.')).toBeTruthy();
    expect(mockSubmitReport.mock.calls[0][0]).toMatchObject({
      kind: 'confusing',
      message: 'the queue jumped',
      screen: 'settings',
    });

    fireEvent.press(screen.getByTestId('report-issue-another'));

    await waitFor(() => expect(screen.getByTestId('report-issue-message').props.value).toBe(''));
    expect(sendDisabled()).toBe(true);
  });

  it('"Done" on the sent screen closes the modal', async () => {
    mockSubmitReport.mockResolvedValue({ issue_number: 7 });
    const { onClose } = renderModal();
    fireEvent.press(screen.getByTestId('report-issue-kind-bug'));
    fireEvent.changeText(screen.getByTestId('report-issue-message'), 'it crashed on play');
    fireEvent.press(screen.getByTestId('report-issue-send'));

    fireEvent.press(await screen.findByTestId('report-issue-done'));

    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('a failed submit shows the error banner and relabels Send as "Try again"', async () => {
    mockSubmitReport.mockRejectedValue(new Error('offline'));
    renderModal();
    fireEvent.press(screen.getByTestId('report-issue-kind-bug'));
    fireEvent.changeText(screen.getByTestId('report-issue-message'), 'it crashed on play');
    fireEvent.press(screen.getByTestId('report-issue-send'));

    expect(await screen.findByTestId('report-issue-error')).toBeTruthy();
    expect(screen.getByText('Try again')).toBeTruthy();
  });

  it('a server 500 shows server-failure copy, not "could not reach" or "saved", and logs the failure', async () => {
    const failure = new ApiError(500, 'API /v1/feedback/reports returned 500');
    mockSubmitReport.mockRejectedValue(failure);
    renderModal();
    fireEvent.press(screen.getByTestId('report-issue-kind-bug'));
    fireEvent.changeText(screen.getByTestId('report-issue-message'), 'it crashed on play');
    fireEvent.press(screen.getByTestId('report-issue-send'));

    expect(
      await screen.findByText(
        'The server had a problem filing your report — try again in a few minutes.',
      ),
    ).toBeTruthy();
    expect(screen.queryByText(/could not reach/i)).toBeNull();
    expect(screen.queryByText(/saved/i)).toBeNull();
    expect(warnSpy).toHaveBeenCalledWith('[feedback] report submission failed', failure);
  });
});

describe('ReportIssueModal(): the form discloses where the message goes', () => {
  it('says the message is filed as an issue in the public GitHub tracker', () => {
    renderModal();
    expect(screen.getByText(/filed as an issue in Altune's public GitHub tracker/)).toBeTruthy();
  });
});

describe('ReportIssueModal(): one draft files one issue, however often it is sent', () => {
  // A dropped response leaves the reporter unable to know whether their issue was
  // filed, and the failed-state UI answers that by relabelling Send as "Try again".
  // The server collapses two submits onto one issue only when both carry the same
  // Idempotency-Key, so without a per-draft key the retry files a second issue (#1755).

  // These tests drive the real submitReport over the fetch double.
  beforeEach(() => {
    mockSubmitReport.mockImplementation(
      jest.requireActual('@shared/api-client/feedback').submitReport,
    );
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
  });

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
});
