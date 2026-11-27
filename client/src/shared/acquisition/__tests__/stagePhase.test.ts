import type { AcquisitionPhase } from '@shared/acquisition';
import { ACQUISITION_PHASES, phaseLabel, stageToPhase } from '@shared/acquisition';

const STAGE_PHASES = [
  ['search', 'finding'],
  ['select', 'finding'],
  ['download', 'downloading'],
  ['tag', 'finishing'],
  ['store', 'finishing'],
  ['update_track', 'finishing'],
] as const;

describe('stageToPhase', () => {
  it('falls back to working for a falsy stage', () => {
    expect(stageToPhase(null)).toBe('working');
    expect(stageToPhase(undefined)).toBe('working');
    expect(stageToPhase('')).toBe('working');
  });

  it('falls back to working for an unrecognized stage', () => {
    expect(stageToPhase('probe_new_step')).toBe('working');
  });

  it.each(STAGE_PHASES)('maps stage %s to phase %s', (stage, phase) => {
    expect(stageToPhase(stage)).toBe(phase);
  });
});

describe('ACQUISITION_PHASES', () => {
  it('lists exactly the three in-progress phases, in progress-bar order', () => {
    expect(ACQUISITION_PHASES).toEqual(['finding', 'downloading', 'finishing']);
  });
});

describe('phaseLabel', () => {
  it.each([
    ['finding', 'Finding source…'],
    ['downloading', 'Downloading…'],
    ['finishing', 'Finishing up…'],
    ['done', 'Done'],
    ['failed', 'Failed'],
    ['working', 'Working…'],
  ] as [AcquisitionPhase, string][])('labels phase %s as %j', (phase, label) => {
    expect(phaseLabel(phase)).toBe(label);
  });
});
