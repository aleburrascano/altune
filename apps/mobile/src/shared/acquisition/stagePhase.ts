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

export function stageLabel(stage: string | null | undefined): string {
  return phaseLabel(stageToPhase(stage));
}
