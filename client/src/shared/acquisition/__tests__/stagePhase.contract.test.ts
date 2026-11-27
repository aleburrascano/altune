import { stageToPhase } from '@shared/acquisition';
import { wireFixture } from '@testing/wireFixtures';

const MAPPED_STAGES = ['search', 'select', 'download', 'tag', 'store', 'update_track'];

function publishedStages(): string[] {
  return wireFixture('download_stages') as string[];
}

describe('the acquisition stages go-api publishes, from the download_stages wire fixture', () => {
  it('are exactly the stages the client maps to a phase', () => {
    expect(publishedStages().length).toBeGreaterThan(0);
    expect([...MAPPED_STAGES].sort()).toEqual([...publishedStages()].sort());
  });

  it.each(publishedStages())(
    '%s maps to a phase of its own, not the unknown-stage fallback',
    (stage) => {
      expect(stageToPhase(stage)).not.toBe(stageToPhase('not-a-published-stage'));
    },
  );
});
