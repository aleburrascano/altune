import { isPending, isReady, type AcquisitionStatus } from '@shared/catalog';

export type LifecyclePhase = 'finding' | 'downloading' | 'finishing' | 'done' | 'failed';

export type LifecycleState = {
  status: AcquisitionStatus | undefined;
  phase: LifecyclePhase | undefined;
};

export type LifecycleEvent =
  | { type: 'started' }
  | { type: 'progress'; phase: LifecyclePhase }
  | { type: 'completed' }
  | { type: 'failed' }
  | { type: 'replaceFailed' }
  | { type: 'response'; status: AcquisitionStatus };

const PHASE_RANK: Record<LifecyclePhase, number> = {
  finding: 0,
  downloading: 1,
  finishing: 2,
  done: 3,
  failed: 3,
};

export function phaseRank(phase: LifecyclePhase): number {
  return PHASE_RANK[phase];
}

function isSettledPhase(phase: LifecyclePhase | undefined): boolean {
  return phase === 'done' || phase === 'failed';
}

function isBehind(phase: LifecyclePhase | undefined, next: LifecyclePhase): boolean {
  if (phase === undefined) return false;
  return isSettledPhase(phase) || phaseRank(next) < phaseRank(phase);
}

function started(state: LifecycleState): LifecycleState | null {
  if (isReady(state.status) || isBehind(state.phase, 'finding')) return null;
  return { status: 'pending', phase: 'finding' };
}

function progressed(state: LifecycleState, phase: LifecyclePhase): LifecycleState | null {
  return isBehind(state.phase, phase) ? null : { status: state.status, phase };
}

function responded(state: LifecycleState, status: AcquisitionStatus): LifecycleState | null {
  const holdsLaterStatus =
    isPending(status) && state.status !== undefined && !isPending(state.status);
  return holdsLaterStatus ? null : { status, phase: state.phase };
}

export function nextLifecycle(state: LifecycleState, event: LifecycleEvent): LifecycleState | null {
  switch (event.type) {
    case 'started':
      return started(state);
    case 'progress':
      return progressed(state, event.phase);
    case 'completed':
      return { status: 'ready', phase: 'finishing' };
    case 'failed':
      return { status: 'failed', phase: 'failed' };
    case 'replaceFailed':
      return { status: 'ready', phase: 'failed' };
    case 'response':
      return responded(state, event.status);
  }
}
