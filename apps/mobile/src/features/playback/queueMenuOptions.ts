import type { ActionSheetOption } from '@shared/ui/primitives/ActionSheet';

export type QueueMenuContext = {
  currentIndex: number;
  queueLength: number;
  moveQueueItem: (from: number, to: number) => void;
  removeFromQueue: (queueIndex: number) => void;
};

function moveOptions(queueIndex: number, ctx: QueueMenuContext): ActionSheetOption[] {
  const firstUpNext = ctx.currentIndex + 1;
  const opts: ActionSheetOption[] = [];
  if (queueIndex !== firstUpNext) {
    opts.push({ label: 'Move to Top', onPress: () => ctx.moveQueueItem(queueIndex, firstUpNext) });
    opts.push({ label: 'Move Up', onPress: () => ctx.moveQueueItem(queueIndex, queueIndex - 1) });
  }
  if (queueIndex !== ctx.queueLength - 1) {
    opts.push({ label: 'Move Down', onPress: () => ctx.moveQueueItem(queueIndex, queueIndex + 1) });
  }
  return opts;
}

export function queueMenuOptions(queueIndex: number, ctx: QueueMenuContext): ActionSheetOption[] {
  return [
    ...moveOptions(queueIndex, ctx),
    {
      label: 'Remove from Queue',
      tone: 'danger',
      onPress: () => ctx.removeFromQueue(queueIndex),
    },
  ];
}
