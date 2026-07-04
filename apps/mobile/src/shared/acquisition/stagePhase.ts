/**
 * Maps the backend's six technical acquisition stages onto three human-facing
 * phases with display copy. The backend reports a raw stage name on each
 * `track_acquisition_progress` event; there is no percentage, so the UI shows
 * the phase, not a number. Unknown/new stage names fall back to 'working' so a
 * backend rename never breaks the UI.
 */

export type AcquisitionPhase =
  | 'finding'
  | 'downloading'
  | 'finishing'
  | 'done'
  | 'failed'
  | 'working';

const STAGE_TO_PHASE: Record<string, AcquisitionPhase> = {
  search: 'finding',
  select: 'finding',
  download: 'downloading',
  tag: 'finishing',
  store: 'finishing',
  update_track: 'finishing',
};

const PHASE_LABEL: Record<AcquisitionPhase, string> = {
  finding: 'Finding source…',
  downloading: 'Downloading…',
  finishing: 'Finishing up…',
  done: 'Done',
  failed: 'Failed',
  working: 'Working…',
};

/** The ordered phases, for a segmented progress indicator. */
export const ACQUISITION_PHASES: readonly AcquisitionPhase[] = [
  'finding',
  'downloading',
  'finishing',
];

export function stageToPhase(stage: string | null | undefined): AcquisitionPhase {
  if (!stage) return 'working';
  return STAGE_TO_PHASE[stage] ?? 'working';
}

export function phaseLabel(phase: AcquisitionPhase): string {
  return PHASE_LABEL[phase];
}

/** Convenience: the display caption for a raw backend stage. */
export function stageLabel(stage: string | null | undefined): string {
  return phaseLabel(stageToPhase(stage));
}
