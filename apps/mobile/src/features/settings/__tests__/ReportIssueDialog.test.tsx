import { act, fireEvent, render, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createElement, type ReactElement, type ReactNode } from 'react';

import { ApiError } from '@shared/api-client';
import { submitReport } from '@shared/api-client/feedback';
import { ThemeProvider } from '@shared/ui';

import { ReportIssueDialog } from '../ui/ReportIssueDialog';

jest.mock('@shared/api-client/feedback', () => ({
  submitReport: jest.fn(),
}));

const submitReportMock = submitReport as jest.MockedFunction<typeof submitReport>;

function renderDialog(onClose: () => void = () => {}): ReturnType<typeof render> {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }): ReactElement =>
    createElement(
      QueryClientProvider,
      { client: qc },
      createElement(ThemeProvider, null, children),
    );
  return render(<ReportIssueDialog visible onClose={onClose} screen="settings" />, { wrapper });
}

beforeEach(() => {
  submitReportMock.mockReset();
});

describe('ReportIssueDialog', () => {
  it('keeps Send disabled until a kind and a real description are given', () => {
    const { getByTestId } = renderDialog();

    expect(getByTestId('report-issue-send').props.accessibilityState.disabled).toBe(true);

    fireEvent.changeText(getByTestId('report-issue-message'), 'three tracks went grey again');
    expect(getByTestId('report-issue-send').props.accessibilityState.disabled).toBe(true);

    fireEvent.press(getByTestId('report-issue-kind-bug'));
    expect(getByTestId('report-issue-send').props.accessibilityState.disabled).toBe(false);
  });

  it('rejects a description that is too short to act on', () => {
    const { getByTestId } = renderDialog();

    fireEvent.press(getByTestId('report-issue-kind-idea'));
    fireEvent.changeText(getByTestId('report-issue-message'), 'broken');

    expect(getByTestId('report-issue-send').props.accessibilityState.disabled).toBe(true);
  });

  it('sends the trimmed message, the chosen kind and the device diagnostics', async () => {
    submitReportMock.mockResolvedValue({ issue_number: 42, issue_url: 'https://x/42' });
    const { getByTestId } = renderDialog();

    fireEvent.press(getByTestId('report-issue-kind-idea'));
    fireEvent.changeText(getByTestId('report-issue-message'), '   let me sort albums by year   ');
    await act(async () => {
      fireEvent.press(getByTestId('report-issue-send'));
    });

    expect(submitReportMock).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'idea',
        message: 'let me sort albums by year',
        screen: 'settings',
        platform: expect.any(String),
      }),
    );
  });

  it('confirms with the issue number once the report lands', async () => {
    submitReportMock.mockResolvedValue({ issue_number: 42, issue_url: 'https://x/42' });
    const { getByTestId, getByText } = renderDialog();

    fireEvent.press(getByTestId('report-issue-kind-bug'));
    fireEvent.changeText(getByTestId('report-issue-message'), 'the player stops between tracks');
    await act(async () => {
      fireEvent.press(getByTestId('report-issue-send'));
    });

    await waitFor(() => expect(getByText(/Filed as #42/)).toBeTruthy());
  });

  it('keeps the draft and explains the failure when the send fails', async () => {
    submitReportMock.mockRejectedValue(new Error('offline'));
    const { getByTestId } = renderDialog();

    fireEvent.press(getByTestId('report-issue-kind-bug'));
    fireEvent.changeText(getByTestId('report-issue-message'), 'the player stops between tracks');
    await act(async () => {
      fireEvent.press(getByTestId('report-issue-send'));
    });

    await waitFor(() => expect(getByTestId('report-issue-error')).toHaveTextContent(/try again/i));
    expect(getByTestId('report-issue-message').props.value).toBe(
      'the player stops between tracks',
    );
  });

  it('names the rate limit rather than blaming the network', async () => {
    submitReportMock.mockRejectedValue(new ApiError(429, 'too many'));
    const { getByTestId } = renderDialog();

    fireEvent.press(getByTestId('report-issue-kind-bug'));
    fireEvent.changeText(getByTestId('report-issue-message'), 'the player stops between tracks');
    await act(async () => {
      fireEvent.press(getByTestId('report-issue-send'));
    });

    await waitFor(() =>
      expect(getByTestId('report-issue-error')).toHaveTextContent(/give it an hour/i),
    );
  });
});
