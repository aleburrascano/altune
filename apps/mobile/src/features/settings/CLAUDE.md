# settings — feature-local router

Grouped-card settings screen: account, the in-app feedback entry point, appearance, offline downloads, library maintenance, and a red-tinted Danger zone. Every destructive action and the report flow go through the feature's own centre dialog, not a native `Alert` or a bottom sheet.

Layout:

- `ui/SettingsScreen.tsx` — orchestrator; owns `reporting` and the single `confirming` discriminant.
- `ui/SettingsCard.tsx` — labelled card group, `danger` variant. `ui/SettingsRow.tsx` — icon + label + detail + right slot, `SettingsRowTone`.
- `ui/ThemeSegment.tsx` — Dark/Light segmented control over `themePreference.setScheme`.
- `ui/FeedbackCard.tsx` — the accent CTA that opens the report dialog.
- `ui/Dialog.tsx` — centre-dialog shell (backdrop, keyboard avoidance, scroll). `ui/ConfirmDialog.tsx`, `ui/ReportIssueDialog.tsx` are its two users.
- `ui/reportDiagnostics.ts` — `reportDiagnostics`, `diagnosticsSummary`.
- `hooks/useSubmitReport.ts` — the mutation plus `submitFailureMessage`. `hooks/useBackfillFeatured.ts`, `hooks/useClearSearchHistory.ts`.
- `__tests__/` — `SettingsScreen`, `ReportIssueDialog`.

Dependencies: `@shared/ui` (plus `primitives/TextField` directly), `@shared/api-client/feedback`, `@shared/auth/{useSession,useSignOut}`, `@shared/offline/{pinnedStore,pinnedFiles}`, `@shared/ui/theme/themePreference`, `lucide-react-native`.

## Rules

- Route every destructive action through `ConfirmDialog` — never act on the first tap, and never reach for `Alert`.
- Keep destructive rows in the `danger` card; never mix one into a neutral group.
- Dialogs are centred; never convert one to a bottom sheet.
- Show the diagnostics the report will carry before it is sent — never attach anything the tester cannot see.
- Keep the report dialog's draft on failure; only a success clears it.
- Read a row's live value from its store, never from a value computed once on mount.
- Keep the whole report flow in this feature: it has one consumer, so it does not move to `shared/`.

Why each rule exists: `okf/mobile/settings-feature.md` — read before structural work; update it in the same commit when behavior it describes changes (pre-commit hook enforces).
