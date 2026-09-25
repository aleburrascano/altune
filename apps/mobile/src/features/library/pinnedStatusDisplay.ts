import { ArrowDownCircle, CircleAlert, CircleCheck, type LucideIcon } from 'lucide-react-native';

import type { PinnedStatus } from '@shared/offline/pinnedStore';

export type PinnedStatusDisplay = {
  a11ySuffix: string;
  icon: {
    glyph: LucideIcon;
    color: 'accent' | 'textTertiary' | 'danger';
    testIdPrefix: string;
  } | null;
  menu: { label: string; action: 'pin' | 'unpin' };
};

const NEVER_DOWNLOADED: PinnedStatusDisplay = {
  a11ySuffix: '',
  icon: null,
  menu: { label: 'Download', action: 'pin' },
};

const IN_FLIGHT: PinnedStatusDisplay = {
  a11ySuffix: ', downloading',
  icon: {
    glyph: ArrowDownCircle,
    color: 'textTertiary',
    testIdPrefix: 'library-row-offline-pending',
  },
  menu: { label: 'Cancel download', action: 'unpin' },
};

const BY_STATUS: Record<PinnedStatus, PinnedStatusDisplay> = {
  ready: {
    a11ySuffix: ', downloaded',
    icon: { glyph: CircleCheck, color: 'accent', testIdPrefix: 'library-row-offline' },
    menu: { label: 'Remove download', action: 'unpin' },
  },
  queued: IN_FLIGHT,
  downloading: IN_FLIGHT,
  failed: {
    a11ySuffix: ', download failed',
    icon: { glyph: CircleAlert, color: 'danger', testIdPrefix: 'library-row-offline-failed' },
    menu: { label: 'Retry download', action: 'pin' },
  },
};

export function pinnedStatusDisplay(status: PinnedStatus | undefined): PinnedStatusDisplay {
  return status === undefined ? NEVER_DOWNLOADED : BY_STATUS[status];
}
