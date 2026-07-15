import { stageToPhase, phaseLabel, stageLabel, ACQUISITION_PHASES } from '../stagePhase';

describe('stageToPhase', () => {
  it('groups search and select into finding', () => {
    expect(stageToPhase('search')).toBe('finding');
    expect(stageToPhase('select')).toBe('finding');
  });

  it('maps download to downloading', () => {
    expect(stageToPhase('download')).toBe('downloading');
  });

  it('groups tag, store, and update_track into finishing', () => {
    expect(stageToPhase('tag')).toBe('finishing');
    expect(stageToPhase('store')).toBe('finishing');
    expect(stageToPhase('update_track')).toBe('finishing');
  });

  it('falls back to working for unknown, null, or undefined stages', () => {
    expect(stageToPhase('some_new_stage')).toBe('working');
    expect(stageToPhase(null)).toBe('working');
    expect(stageToPhase(undefined)).toBe('working');
  });
});

describe('phaseLabel / stageLabel', () => {
  it('gives human copy per phase', () => {
    expect(phaseLabel('finding')).toBe('Finding source…');
    expect(phaseLabel('downloading')).toBe('Downloading…');
    expect(phaseLabel('finishing')).toBe('Finishing up…');
    expect(phaseLabel('working')).toBe('Working…');
  });

  it('maps a raw stage straight to its caption', () => {
    expect(stageLabel('download')).toBe('Downloading…');
    expect(stageLabel('unknown')).toBe('Working…');
  });
});

describe('ACQUISITION_PHASES', () => {
  it('is the three ordered visible phases (excludes the working fallback)', () => {
    expect(ACQUISITION_PHASES).toEqual(['finding', 'downloading', 'finishing']);
  });
});
