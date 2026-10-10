import { asPlaylistId } from '@shared/apiClient/ids';
import type { QueueSource } from '@shared/playback/queueOrder';

import {
  buildUpNextItems,
  queueMenuOptions,
  queueSourceLabel,
  type QueueMenuContext,
} from '@features/playback/domain/upNextQueue';
import { libraryTrack } from '@features/playback/__tests__/fixtures';

describe('queueSourceLabel', () => {
  it.each<[string, QueueSource | null, string]>([
    ['null', null, 'Queue'],
    ['library', { kind: 'library' }, 'Playing from Library'],
    ['search', { kind: 'search', query: 'q' }, 'Playing from search'],
    [
      'playlist',
      { kind: 'playlist', playlistId: asPlaylistId('pl-1'), name: 'Road' },
      'Playing from Road',
    ],
  ])('labels %s source', (_name, source, label) => {
    expect(queueSourceLabel(source)).toBe(label);
  });
});

describe('buildUpNextItems', () => {
  const tracks = [
    libraryTrack({ title: 'A', durationSeconds: 60 }),
    libraryTrack({ title: 'B' }),
    libraryTrack({ title: 'C' }),
  ];

  it('returns nothing for an empty queue', () => {
    expect(buildUpNextItems([], [], -1)).toEqual([]);
  });

  it('returns nothing when the current track is last', () => {
    expect(buildUpNextItems(tracks, [0, 1, 2], 2)).toEqual([]);
  });

  it('follows the shuffled play order after the current index', () => {
    const items = buildUpNextItems(tracks, [2, 0, 1], 0);
    expect(items.map((i) => [i.title, i.trackIndex, i.queueIndex])).toEqual([
      ['A', 0, 1],
      ['B', 1, 2],
    ]);
  });

  it('carries duration and skips play-order entries with no track', () => {
    const items = buildUpNextItems(tracks, [1, 0, 9], 0);
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ title: 'A', durationSeconds: 60, queueIndex: 1 });
  });
});

function makeCtx(over: Partial<QueueMenuContext> = {}): QueueMenuContext {
  return {
    currentIndex: 2,
    queueLength: 8,
    moveQueueItem: jest.fn(),
    removeFromQueue: jest.fn(),
    ...over,
  };
}

const labels = (queueIndex: number, ctx: QueueMenuContext): string[] =>
  queueMenuOptions(queueIndex, ctx).map((o) => o.label);

function press(queueIndex: number, ctx: QueueMenuContext, label: string): void {
  const opt = queueMenuOptions(queueIndex, ctx).find((o) => o.label === label);
  if (!opt) throw new Error(`missing option ${label}`);
  opt.onPress();
}

describe('queueMenuOptions', () => {
  it('offers every move for a middle item, remove last and danger-toned', () => {
    const ctx = makeCtx();
    const opts = queueMenuOptions(5, ctx);
    expect(opts.map((o) => o.label)).toEqual([
      'Move to Top',
      'Move Up',
      'Move Down',
      'Remove from Queue',
    ]);
    expect(opts[3]?.tone).toBe('danger');
  });

  it('hides Move to Top and Move Up for the first up-next item', () => {
    expect(labels(3, makeCtx())).toEqual(['Move Down', 'Remove from Queue']);
  });

  it('hides Move Down for the last item in the queue', () => {
    expect(labels(7, makeCtx())).toEqual(['Move to Top', 'Move Up', 'Remove from Queue']);
  });

  it('offers only Remove when the item is both first and last', () => {
    expect(labels(3, makeCtx({ queueLength: 4 }))).toEqual(['Remove from Queue']);
  });

  it('wires each option to the right queue command', () => {
    const ctx = makeCtx();
    press(5, ctx, 'Move to Top');
    press(5, ctx, 'Move Up');
    press(5, ctx, 'Move Down');
    press(5, ctx, 'Remove from Queue');
    expect(ctx.moveQueueItem).toHaveBeenNthCalledWith(1, 5, 3);
    expect(ctx.moveQueueItem).toHaveBeenNthCalledWith(2, 5, 4);
    expect(ctx.moveQueueItem).toHaveBeenNthCalledWith(3, 5, 6);
    expect(ctx.removeFromQueue).toHaveBeenCalledWith(5);
  });
});
