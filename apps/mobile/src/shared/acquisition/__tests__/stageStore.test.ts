import { useAcquisitionStageStore, setTrackStage, clearTrackStage } from '../stageStore';

beforeEach(() => {
  useAcquisitionStageStore.setState({ stages: {} });
});

describe('acquisition stage store', () => {
  it('sets a stage for a track', () => {
    setTrackStage('t1', 'download');
    expect(useAcquisitionStageStore.getState().stages.t1).toBe('download');
  });

  it('overwrites the stage on a later set', () => {
    setTrackStage('t1', 'search');
    setTrackStage('t1', 'download');
    expect(useAcquisitionStageStore.getState().stages.t1).toBe('download');
  });

  it('clears a track stage', () => {
    setTrackStage('t1', 'download');
    clearTrackStage('t1');
    expect(useAcquisitionStageStore.getState().stages.t1).toBeUndefined();
  });

  it('clearing an absent track is a no-op', () => {
    expect(() => clearTrackStage('ghost')).not.toThrow();
    expect(useAcquisitionStageStore.getState().stages).toEqual({});
  });
});
