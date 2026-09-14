import type { ReportKind } from '@shared/api-client/feedback';

export const MIN_MESSAGE_LENGTH = 10;
export const MAX_MESSAGE_LENGTH = 2000;

export function isReportReady(kind: ReportKind | null, message: string): boolean {
  return kind !== null && message.trim().length >= MIN_MESSAGE_LENGTH;
}
