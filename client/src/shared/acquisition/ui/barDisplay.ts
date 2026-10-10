import {
  aggregatePhase,
  isInFlight,
  type DownloadEntry,
  type DownloadPhase,
} from '@shared/acquisition/downloadStore';
import { ACQUISITION_PHASES } from '@shared/acquisition/stagePhase';

export interface BarDisplay {
  phase: DownloadPhase;
  activeIndex: number;
  count: number;
  heading: string;
}

export function deriveBarDisplay(items: DownloadEntry[]): BarDisplay {
  const phase = aggregatePhase(items) ?? 'finding';
  const inFlight = items.filter(isInFlight);
  const failed = items.filter((i) => i.phase === 'failed').length;
  const failedSuffix = failed > 0 ? `${failed} failed` : '';

  if (inFlight.length === 0 && items.length > 0) {
    const allFailed = failed === items.length;
    const heading = allFailed ? failedSuffix : failed > 0 ? `Done, ${failedSuffix}` : 'Done';
    return { phase, activeIndex: ACQUISITION_PHASES.length, count: items.length, heading };
  }

  const count = inFlight.length;
  const downloading =
    count === 1 ? `Downloading "${inFlight[0]?.title ?? 'track'}"` : `Downloading ${count} tracks`;
  const heading = failed > 0 ? `${downloading}, ${failedSuffix}` : downloading;
  return { phase, activeIndex: ACQUISITION_PHASES.indexOf(phase), count, heading };
}
