import { apiFetch } from './index';

export type ReportKind = 'bug' | 'idea' | 'confusing';

export type SubmitReportInput = {
  kind: ReportKind;
  message: string;
  app_version: string;
  platform: string;
  os_version: string;
  screen: string;
};

export type SubmitReportResponse = {
  issue_number: number;
  issue_url: string;
};

export async function submitReport(input: SubmitReportInput): Promise<SubmitReportResponse> {
  return apiFetch<SubmitReportResponse>('/v1/feedback/reports', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  });
}
