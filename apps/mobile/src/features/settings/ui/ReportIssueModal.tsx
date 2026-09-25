import { useRef, useState, type ReactElement } from 'react';

import { makeReportIdempotencyKey } from '@shared/api-client/feedback';
import type { ReportKind } from '@shared/api-client/feedback';
import { failureCopyForReport } from '../failureCopyForReport';
import { useSubmitReport } from '../hooks/useSubmitReport';
import { SettingsModal } from './SettingsModal';
import { ReportFormView } from './ReportFormView';
import { ReportSentView } from './ReportSentView';
import { reportDiagnostics } from './reportDiagnostics';
import { isReportReady } from './reportRules';

type ReportIssueModalProps = {
  visible: boolean;
  onClose: () => void;
  screen: string;
};

export function ReportIssueModal({
  visible,
  onClose,
  screen,
}: ReportIssueModalProps): ReactElement {
  const submit = useSubmitReport();
  const [kind, setKind] = useState<ReportKind | null>(null);
  const [message, setMessage] = useState('');
  const idempotencyKeyRef = useRef<string | null>(null);
  const lastPayloadRef = useRef<string | null>(null);

  const diagnostics = reportDiagnostics(screen);

  // One key per draft, so every submit of this draft — a manual "Try again"
  // after an ambiguous timeout, or a double-tapped Send — is the same
  // submission to the server and can only ever file one issue.
  const draftIdempotencyKey = (payload: string): string => {
    if (lastPayloadRef.current !== payload) idempotencyKeyRef.current = null;
    lastPayloadRef.current = payload;
    idempotencyKeyRef.current ??= makeReportIdempotencyKey();
    return idempotencyKeyRef.current;
  };

  const clearDraft = (): void => {
    submit.reset();
    setKind(null);
    setMessage('');
    idempotencyKeyRef.current = null;
    lastPayloadRef.current = null;
  };

  const close = (): void => {
    onClose();
    if (submit.isSuccess) clearDraft();
  };

  const send = (): void => {
    if (kind === null) return;
    const trimmed = message.trim();
    submit.mutate({
      kind,
      message: trimmed,
      ...diagnostics,
      idempotencyKey: draftIdempotencyKey(JSON.stringify([kind, trimmed])),
    });
  };

  return (
    <SettingsModal visible={visible} onClose={close} testID="report-issue-modal">
      {submit.isSuccess ? (
        <ReportSentView
          issueNumber={submit.data.issue_number}
          onDone={close}
          onSendAnother={clearDraft}
        />
      ) : (
        <ReportFormView
          kind={kind}
          onKindChange={setKind}
          message={message}
          onMessageChange={setMessage}
          diagnostics={diagnostics}
          failure={submit.isError ? failureCopyForReport(submit.error) : null}
          ready={isReportReady(kind, message)}
          sending={submit.isPending}
          onCancel={close}
          onSend={send}
        />
      )}
    </SettingsModal>
  );
}
