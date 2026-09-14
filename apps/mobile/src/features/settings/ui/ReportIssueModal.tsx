import { useState, type ReactElement } from 'react';

import type { ReportKind } from '@shared/api-client/feedback';
import { submitFailureMessage, useSubmitReport } from '../hooks/useSubmitReport';
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

  const diagnostics = reportDiagnostics(screen);

  const clearDraft = (): void => {
    submit.reset();
    setKind(null);
    setMessage('');
  };

  const close = (): void => {
    onClose();
    if (submit.isSuccess) clearDraft();
  };

  const send = (): void => {
    if (kind === null) return;
    submit.mutate({ kind, message: message.trim(), ...diagnostics });
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
          failure={submit.isError ? submitFailureMessage(submit.error) : null}
          ready={isReportReady(kind, message)}
          sending={submit.isPending}
          onCancel={close}
          onSend={send}
        />
      )}
    </SettingsModal>
  );
}
