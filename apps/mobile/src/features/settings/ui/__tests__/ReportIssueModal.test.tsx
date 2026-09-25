import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/api-client';
import { submitReport } from '@shared/api-client/feedback';
import { ReportIssueModal } from '../ReportIssueModal';

jest.mock('@shared/api-client/feedback', () => ({
  ...jest.requireActual('@shared/api-client/feedback'),
  submitReport: jest.fn(),
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
