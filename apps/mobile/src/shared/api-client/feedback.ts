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

// makeReportIdempotencyKey mints a fresh UUID v4 to tag one report draft. The
// server files one issue per distinct key within a 30-minute window and a fresh
// issue for every keyless submit (feedback/adapters/handler/feedback_handler.go),
// so a draft that keeps its key across a retry cannot file a second issue. The
// v4 comes from expo-crypto rather than Math.random for the same reason the
// track minter does (#1774): Math.random's state is recoverable from earlier
// keys, and two drafts colliding would hand one reporter the other's issue.
export function makeReportIdempotencyKey(): string {
  return Crypto.randomUUID();
}

// Both fields are shown to the reporter as the filed issue's confirmation, so an
// off-contract body fails here rather than rendering "Filed issue #undefined".
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
