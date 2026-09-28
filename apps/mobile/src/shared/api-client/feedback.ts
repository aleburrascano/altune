import * as Crypto from 'expo-crypto';

import { apiSend } from './index';
import { asNumber, asRecord, asString } from './wireDecoders';

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

export function makeReportIdempotencyKey(): string {
  return Crypto.randomUUID();
}

function parseSubmitReportResponse(
  value: unknown,
  at = 'SubmitReportResponse',
): SubmitReportResponse {
  const r = asRecord(value, at);
  return {
    issue_number: asNumber(r.issue_number, `${at}.issue_number`),
    issue_url: asString(r.issue_url, `${at}.issue_url`),
  };
}

export async function submitReport(
  input: SubmitReportInput,
  idempotencyKey: string = makeReportIdempotencyKey(),
): Promise<SubmitReportResponse> {
  return parseSubmitReportResponse(
    await apiSend<unknown>('/v1/feedback/reports', 'POST', input, {
      headers: { 'Idempotency-Key': idempotencyKey },
    }),
  );
}
