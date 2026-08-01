import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react-native';

import { AddToPlaylistSheet } from '../AddToPlaylistSheet';
import { playlistKeys } from '@shared/lib/query-keys';
import type { PlaylistResponse } from '@shared/api-client/types';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

function playlist(overrides: Partial<PlaylistResponse> = {}): PlaylistResponse {
  return {
    id: 'p1',
    name: 'Focus',
    track_count: 3,
    preview_artwork_urls: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function freshClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

type RenderOptions = {
  visible?: boolean;
  label?: string;
  resolveTrackIds?: () => Promise<string[]>;
  queryClient?: QueryClient;
};

function renderSheet(options: RenderOptions = {}) {
  const queryClient = options.queryClient ?? freshClient();
  const onClose = jest.fn();
  const resolveTrackIds = options.resolveTrackIds ?? (() => Promise.resolve(['t1']));
  const utils = render(
    <QueryClientProvider client={queryClient}>
      <AddToPlaylistSheet
        visible={options.visible ?? true}
        label={options.label ?? '1 track'}
        resolveTrackIds={resolveTrackIds}
        onClose={onClose}
      />
    </QueryClientProvider>,
  );
  return { ...utils, onClose, queryClient };
}

let alertSpy: jest.SpyInstance;

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
});

afterEach(() => {
  alertSpy.mockRestore();
});

describe('AddToPlaylistSheet(): withTrackIds rejecting closes the sheet and never reaches the add endpoint (:57-70)', () => {
  it('a resolveTrackIds rejection, from a Track that failed to save, closes the sheet without ever calling the batch-add endpoint', async () => {
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: { items: [playlist({ id: 'p1', name: 'Focus' })], total: 1 },
    });
    const resolveTrackIds = jest.fn().mockRejectedValue(new Error('save failed'));
    const { onClose } = renderSheet({ resolveTrackIds });

    await waitFor(() => screen.getByTestId('add-to-playlist-p1'));
    fireEvent.press(screen.getByTestId('add-to-playlist-p1'));

    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(__http.countFor('POST /v1/playlists/p1/tracks/batch')).toBe(0);
  });
});

describe("AddToPlaylistSheet(): withTrackIds's trackIds.length > 0 guard (:62)", () => {
  it('an empty resolved array fires no add request and leaves the sheet open, untouched', async () => {
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: { items: [playlist({ id: 'p1', name: 'Focus' })], total: 1 },
    });
    const resolveTrackIds = jest.fn().mockResolvedValue([]);
    const { onClose } = renderSheet({ resolveTrackIds });

    await waitFor(() => screen.getByTestId('add-to-playlist-p1'));
    fireEvent.press(screen.getByTestId('add-to-playlist-p1'));

    await waitFor(() => expect(resolveTrackIds).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.queryByTestId('add-to-playlist-busy')).toBeNull());

    expect(onClose).not.toHaveBeenCalled();
    expect(__http.countFor('POST /v1/playlists/p1/tracks/batch')).toBe(0);
    expect(screen.getByTestId('add-to-playlist-sheet').props.visible).toBe(true);
  });
});

describe('AddToPlaylistSheet(): functional — picking a playlist adds exactly the ids resolveTrackIds produced', () => {
  it('a Track id resolved only at pick time is the id sent to the batch-add endpoint', async () => {
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: { items: [playlist({ id: 'p1', name: 'Focus' })], total: 1 },
    });
    __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json: { added: 1, skipped: 0 } });
    const resolveTrackIds = jest.fn().mockResolvedValue(['track-saved-just-now']);
    renderSheet({ resolveTrackIds });

    await waitFor(() => screen.getByTestId('add-to-playlist-p1'));
    fireEvent.press(screen.getByTestId('add-to-playlist-p1'));

    await waitFor(() => expect(__http.countFor('POST /v1/playlists/p1/tracks/batch')).toBe(1));
    const addRequest = __http.requests.find(
      (r: { path: string }) => r.path === '/v1/playlists/p1/tracks/batch',
    );
    expect(JSON.parse(addRequest.body)).toEqual({ track_ids: ['track-saved-just-now'] });
  });

  it('one gesture over a bulk selection of many Tracks produces exactly one batch request, not one per track', async () => {
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: { items: [playlist({ id: 'p1', name: 'Focus' })], total: 1 },
    });
    __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json: { added: 5, skipped: 0 } });
    const ids = ['t1', 't2', 't3', 't4', 't5'];
    renderSheet({ resolveTrackIds: () => Promise.resolve(ids) });

    await waitFor(() => screen.getByTestId('add-to-playlist-p1'));
    fireEvent.press(screen.getByTestId('add-to-playlist-p1'));

    await waitFor(() => expect(__http.countFor('POST /v1/playlists/p1/tracks/batch')).toBe(1));
    const addRequest = __http.requests.find(
      (r: { path: string }) => r.path === '/v1/playlists/p1/tracks/batch',
    );
    expect(JSON.parse(addRequest.body)).toEqual({ track_ids: ids });
  });
});

describe('AddToPlaylistSheet(): liveness — renders playlist track counts live from the query cache, not a snapshot', () => {
  it('a track_count patched into playlistKeys.list while the sheet is open updates the rendered row without new props', async () => {
    const queryClient = freshClient();
    queryClient.setQueryData(playlistKeys.list, {
      items: [playlist({ id: 'p1', name: 'Focus', track_count: 3 })],
      total: 1,
    });
    renderSheet({ queryClient });

    expect(await screen.findByText('3 tracks')).toBeTruthy();

    await act(async () => {
      queryClient.setQueryData(playlistKeys.list, {
        items: [playlist({ id: 'p1', name: 'Focus', track_count: 5 })],
        total: 1,
      });
      await Promise.resolve();
    });

    expect(await screen.findByText('5 tracks')).toBeTruthy();
    expect(screen.queryByText('3 tracks')).toBeNull();
  });
});

describe("AddToPlaylistSheet(): busy = resolving || addMut.isPending || createMut.isPending (:55)", () => {
  it('resolving alone keeps the picker busy while resolveTrackIds is in flight', async () => {
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: { items: [playlist({ id: 'p1', name: 'Focus' })], total: 1 },
    });
    __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json: { added: 1, skipped: 0 } });
    let resolveIds!: (ids: string[]) => void;
    const pending = new Promise<string[]>((resolve) => {
      resolveIds = resolve;
    });
    renderSheet({ resolveTrackIds: () => pending });

    await waitFor(() => screen.getByTestId('add-to-playlist-p1'));
    fireEvent.press(screen.getByTestId('add-to-playlist-p1'));

    await waitFor(() => expect(screen.getByTestId('add-to-playlist-busy')).toBeTruthy());
    expect(screen.getByTestId('add-to-playlist-p1').props.accessibilityState.disabled).toBe(true);
    expect(screen.getByTestId('add-to-playlist-create-new').props.accessibilityState.disabled).toBe(true);

    await act(async () => {
      resolveIds(['t1']);
      await pending;
    });

    await waitFor(() => expect(screen.queryByTestId('add-to-playlist-busy')).toBeNull());
  });

  it("addMut.isPending alone keeps the picker busy after resolveTrackIds has already settled", async () => {
    jest.useFakeTimers();
    try {
      __http.reply('GET /v1/playlists', {
        status: 200,
        json: { items: [playlist({ id: 'p1', name: 'Focus' })], total: 1 },
      });
      __http.hang('POST /v1/playlists/p1/tracks/batch');
      renderSheet({ resolveTrackIds: () => Promise.resolve(['t1']) });

      await act(async () => {
        await jest.advanceTimersByTimeAsync(0);
      });
      fireEvent.press(screen.getByTestId('add-to-playlist-p1'));
      await act(async () => {
        await jest.advanceTimersByTimeAsync(0);
      });

      expect(screen.getByTestId('add-to-playlist-busy')).toBeTruthy();
      expect(screen.getByTestId('add-to-playlist-p1').props.accessibilityState.disabled).toBe(true);

      await act(async () => {
        await jest.advanceTimersByTimeAsync(15_000);
      });
    } finally {
      jest.useRealTimers();
    }
  });

  it("createMut.isPending alone marks the create modal's confirm button busy, independent of the picker", async () => {
    jest.useFakeTimers();
    try {
      __http.reply('GET /v1/playlists', { status: 200, json: { items: [], total: 0 } });
      __http.hang('POST /v1/playlists');
      renderSheet();

      await act(async () => {
        await jest.advanceTimersByTimeAsync(0);
      });
      fireEvent.press(screen.getByTestId('add-to-playlist-create-new'));
      fireEvent.changeText(screen.getByTestId('create-playlist-input'), 'Road Trip Mix');
      fireEvent.press(screen.getByTestId('create-playlist-confirm'));
      await act(async () => {
        await jest.advanceTimersByTimeAsync(0);
      });

      expect(screen.getByTestId('create-playlist-confirm').props.accessibilityState.busy).toBe(true);

      await act(async () => {
        await jest.advanceTimersByTimeAsync(15_000);
      });
    } finally {
      jest.useRealTimers();
    }
  });
});

describe('AddToPlaylistSheet(): which modal is on screen tracks visible && !createVisible (:155)', () => {
  it('shows the picker and keeps the create modal unmounted until requested', async () => {
    __http.reply('GET /v1/playlists', { status: 200, json: { items: [], total: 0 } });
    renderSheet();

    await waitFor(() => expect(screen.getByTestId('add-to-playlist-sheet').props.visible).toBe(true));
    expect(screen.queryByTestId('create-playlist-modal')).toBeNull();
  });

  it('flips to the create modal when "Create New Playlist" is pressed, hiding the picker', async () => {
    __http.reply('GET /v1/playlists', { status: 200, json: { items: [], total: 0 } });
    renderSheet();

    await waitFor(() => screen.getByTestId('add-to-playlist-create-new'));
    fireEvent.press(screen.getByTestId('add-to-playlist-create-new'));

    expect(screen.queryByTestId('add-to-playlist-sheet')).toBeNull();
    expect(screen.getByTestId('create-playlist-modal').props.visible).toBe(true);
  });
});

describe('AddToPlaylistSheet(): the playlist query is enabled only while visible (:46-51)', () => {
  it('an invisible sheet still issues no GET after the async auth chain has had time to settle; becoming visible fires exactly one', async () => {
    jest.useFakeTimers();
    try {
      __http.reply('GET /v1/playlists', { status: 200, json: { items: [], total: 0 } });
      const queryClient = freshClient();
      const { rerender } = render(
        <QueryClientProvider client={queryClient}>
          <AddToPlaylistSheet
            visible={false}
            label="1 track"
            resolveTrackIds={() => Promise.resolve(['t1'])}
            onClose={jest.fn()}
          />
        </QueryClientProvider>,
      );

      // Let the whole async chain a wrongly-enabled query would run through
      // (supabase.auth.getSession() -> apiFetch -> fetch) actually settle,
      // instead of only checking the instant before any of it could resolve.
      await act(async () => {
        await jest.advanceTimersByTimeAsync(0);
      });
      await act(async () => {
        await jest.advanceTimersByTimeAsync(0);
      });

      expect(__http.countFor('GET /v1/playlists')).toBe(0);
      expect(__http.countFor('POST /v1/playlists/p1/tracks/batch')).toBe(0);

      rerender(
        <QueryClientProvider client={queryClient}>
          <AddToPlaylistSheet
            visible
            label="1 track"
            resolveTrackIds={() => Promise.resolve(['t1'])}
            onClose={jest.fn()}
          />
        </QueryClientProvider>,
      );

      await act(async () => {
        await jest.advanceTimersByTimeAsync(0);
      });

      expect(__http.countFor('GET /v1/playlists')).toBe(1);
    } finally {
      jest.useRealTimers();
    }
  });
});

describe('AddToPlaylistSheet(): ListEmptyComponent only once loading has settled (:205-213)', () => {
  it('shows nothing while the playlists query is still in flight', async () => {
    jest.useFakeTimers();
    try {
      __http.hang('GET /v1/playlists');
      renderSheet();

      await act(async () => {
        await jest.advanceTimersByTimeAsync(0);
      });

      expect(screen.queryByText('No playlists yet')).toBeNull();

      await act(async () => {
        await jest.advanceTimersByTimeAsync(15_000);
      });
    } finally {
      jest.useRealTimers();
    }
  });

  it('shows "No playlists yet" once the query settles with zero playlists', async () => {
    __http.reply('GET /v1/playlists', { status: 200, json: { items: [], total: 0 } });
    renderSheet();

    expect(await screen.findByText('No playlists yet')).toBeTruthy();
  });
});

describe('AddToPlaylistSheet(): renderPlaylistItem accessibility and pluralisation (:108-144)', () => {
  it('labels a row with its name and singular/plural track count, and disables it while busy', async () => {
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: {
        items: [
          playlist({ id: 'p1', name: 'Focus', track_count: 1 }),
          playlist({ id: 'p2', name: 'Chill', track_count: 4 }),
        ],
        total: 2,
      },
    });

    renderSheet();

    expect(await screen.findByRole('button', { name: 'Add to Focus, 1 track' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Add to Chill, 4 tracks' })).toBeTruthy();
    expect(screen.getByTestId('add-to-playlist-p1').props.accessibilityState.disabled).toBe(false);
  });

  it("marks the row's accessibilityState.disabled true while its own add is in flight, not just its disabled prop", async () => {
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: { items: [playlist({ id: 'p1', name: 'Focus' })], total: 1 },
    });
    let resolveIds!: (ids: string[]) => void;
    const pending = new Promise<string[]>((resolve) => {
      resolveIds = resolve;
    });
    renderSheet({ resolveTrackIds: () => pending });

    await waitFor(() => screen.getByTestId('add-to-playlist-p1'));
    expect(screen.getByTestId('add-to-playlist-p1').props.accessibilityState.disabled).toBe(false);

    fireEvent.press(screen.getByTestId('add-to-playlist-p1'));

    await waitFor(() =>
      expect(screen.getByTestId('add-to-playlist-p1').props.accessibilityState.disabled).toBe(true),
    );

    await act(async () => {
      resolveIds(['t1']);
      await pending;
    });
  });

  it('pluralises a zero-Track playlist as "tracks" — the boundary === 1 alone gets wrong (the label at :118 and the visible text at :136 are two separate expressions)', async () => {
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: { items: [playlist({ id: 'p0', name: 'Empty Crate', track_count: 0 })], total: 1 },
    });

    renderSheet();

    expect(await screen.findByRole('button', { name: 'Add to Empty Crate, 0 tracks' })).toBeTruthy();
    expect(within(screen.getByTestId('add-to-playlist-p0')).getByText('0 tracks')).toBeTruthy();
  });
});

describe('AddToPlaylistSheet(): accessibility — both close affordances are independently reachable', () => {
  it('the picker backdrop exposes its own "Close" button', async () => {
    __http.reply('GET /v1/playlists', { status: 200, json: { items: [], total: 0 } });
    const { onClose } = renderSheet();

    await waitFor(() =>
      expect(within(screen.getByTestId('add-to-playlist-sheet')).getByRole('button', { name: 'Close' })).toBeTruthy(),
    );
    fireEvent.press(within(screen.getByTestId('add-to-playlist-sheet')).getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('the create-playlist backdrop exposes its own, independent "Close" button once opened', async () => {
    __http.reply('GET /v1/playlists', { status: 200, json: { items: [], total: 0 } });
    renderSheet();

    await waitFor(() => screen.getByTestId('add-to-playlist-create-new'));
    fireEvent.press(screen.getByTestId('add-to-playlist-create-new'));

    expect(
      within(screen.getByTestId('create-playlist-modal')).getByRole('button', { name: 'Close' }),
    ).toBeTruthy();
  });
});

describe('AddToPlaylistSheet(): failure injection', () => {
  it('GET /v1/playlists failing does not crash the sheet and never fires an add request', async () => {
    __http.fail('GET /v1/playlists');
    const { onClose } = renderSheet();

    await waitFor(() => expect(screen.queryByTestId('add-to-playlist-busy')).toBeNull());
    expect(__http.requests.filter((r: { path: string }) => r.path.includes('/tracks/batch'))).toHaveLength(0);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('a failed add alerts the user, leaves the sheet open, and never shows a false confirmation', async () => {
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: { items: [playlist({ id: 'p1', name: 'Focus' })], total: 1 },
    });
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    const { onClose } = renderSheet({ resolveTrackIds: () => Promise.resolve(['t1']) });

    await waitFor(() => screen.getByTestId('add-to-playlist-p1'));
    fireEvent.press(screen.getByTestId('add-to-playlist-p1'));

    await waitFor(() =>
      expect(alertSpy).toHaveBeenCalledWith(
        'Add failed',
        'Could not add the track to the playlist. Please try again.',
      ),
    );
    expect(screen.queryByText('Added ✓')).toBeNull();
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe('AddToPlaylistSheet(): the 700ms confirmation dwell is a real hold, not just an eventual close (:72-90)', () => {
  it('keeps "Added ✓" and the sheet open at the midpoint, then closes exactly once past the boundary', async () => {
    jest.useFakeTimers();
    try {
      __http.reply('GET /v1/playlists', {
        status: 200,
        json: { items: [playlist({ id: 'p1', name: 'Focus' })], total: 1 },
      });
      __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json: { added: 1, skipped: 0 } });
      const { onClose } = renderSheet({ resolveTrackIds: () => Promise.resolve(['t1']) });

      await act(async () => {
        await jest.advanceTimersByTimeAsync(0);
      });
      fireEvent.press(screen.getByTestId('add-to-playlist-p1'));
      await act(async () => {
        await jest.advanceTimersByTimeAsync(0);
      });

      expect(screen.getByText('Added ✓')).toBeTruthy();

      await act(async () => {
        await jest.advanceTimersByTimeAsync(350);
      });
      expect(screen.getByText('Added ✓')).toBeTruthy();
      expect(onClose).not.toHaveBeenCalled();

      await act(async () => {
        await jest.advanceTimersByTimeAsync(400);
      });
      expect(screen.queryByText('Added ✓')).toBeNull();
      expect(onClose).toHaveBeenCalledTimes(1);
    } finally {
      jest.useRealTimers();
    }
  });
});

describe('AddToPlaylistSheet(): unmounting clears a pending confirmation timer (:39-44)', () => {
  it('an unmount mid-window prevents the orphaned timer from ever calling onClose again', async () => {
    jest.useFakeTimers();
    try {
      __http.reply('GET /v1/playlists', {
        status: 200,
        json: { items: [playlist({ id: 'p1', name: 'Focus' })], total: 1 },
      });
      __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json: { added: 1, skipped: 0 } });
      const { onClose, unmount } = renderSheet({ resolveTrackIds: () => Promise.resolve(['t1']) });

      await act(async () => {
        await jest.advanceTimersByTimeAsync(0);
      });
      fireEvent.press(screen.getByTestId('add-to-playlist-p1'));
      await act(async () => {
        await jest.advanceTimersByTimeAsync(0);
      });

      expect(screen.getByText('Added ✓')).toBeTruthy();
      expect(onClose).not.toHaveBeenCalled();

      unmount();

      await act(async () => {
        await jest.advanceTimersByTimeAsync(700);
      });

      expect(onClose).not.toHaveBeenCalled();
    } finally {
      jest.useRealTimers();
    }
  });
});

describe('AddToPlaylistSheet(): a second add before the first timer fires cancels the first timer rather than orphaning it', () => {
  it(
    "does not cut short the second add's own 700ms confirmation window",
    async () => {
      jest.useFakeTimers();
      try {
        __http.reply('GET /v1/playlists', {
          status: 200,
          json: {
            items: [playlist({ id: 'p1', name: 'Focus' }), playlist({ id: 'p2', name: 'Chill' })],
            total: 2,
          },
        });
        __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json: { added: 1, skipped: 0 } });
        __http.reply('POST /v1/playlists/p2/tracks/batch', { status: 200, json: { added: 1, skipped: 0 } });
        renderSheet({ resolveTrackIds: () => Promise.resolve(['t1']) });

        await act(async () => {
          await jest.advanceTimersByTimeAsync(0);
        });
        fireEvent.press(screen.getByTestId('add-to-playlist-p1'));
        await act(async () => {
          await jest.advanceTimersByTimeAsync(0);
        });
        expect(screen.getByText('Added ✓')).toBeTruthy();

        await act(async () => {
          await jest.advanceTimersByTimeAsync(300);
        });
        fireEvent.press(screen.getByTestId('add-to-playlist-p2'));
        await act(async () => {
          await jest.advanceTimersByTimeAsync(0);
        });
        expect(screen.getByText('Added ✓')).toBeTruthy();

        await act(async () => {
          await jest.advanceTimersByTimeAsync(400);
        });

        expect(screen.getByText('Added ✓')).toBeTruthy();
      } finally {
        jest.useRealTimers();
      }
    },
  );
});

describe('AddToPlaylistSheet(): handleClose cancels a pending confirmation timer (:146-149)', () => {
  it(
    'a manual close is the final call to onClose — no further, delayed call comes from the orphaned timer',
    async () => {
      jest.useFakeTimers();
      try {
        __http.reply('GET /v1/playlists', {
          status: 200,
          json: { items: [playlist({ id: 'p1', name: 'Focus' })], total: 1 },
        });
        __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json: { added: 1, skipped: 0 } });
        const { onClose } = renderSheet({ resolveTrackIds: () => Promise.resolve(['t1']) });

        await act(async () => {
          await jest.advanceTimersByTimeAsync(0);
        });
        fireEvent.press(screen.getByTestId('add-to-playlist-p1'));
        await act(async () => {
          await jest.advanceTimersByTimeAsync(0);
        });
        expect(screen.getByText('Added ✓')).toBeTruthy();

        fireEvent.press(within(screen.getByTestId('add-to-playlist-sheet')).getByRole('button', { name: 'Close' }));
        expect(onClose).toHaveBeenCalledTimes(1);

        await act(async () => {
          await jest.advanceTimersByTimeAsync(700);
        });

        expect(onClose).toHaveBeenCalledTimes(1);
      } finally {
        jest.useRealTimers();
      }
    },
  );
});

describe('AddToPlaylistSheet(): createAndAdd (:92-104)', () => {
  it('a successful create-and-add closes both the create modal and the sheet', async () => {
    __http.reply('GET /v1/playlists', { status: 200, json: { items: [], total: 0 } });
    __http.reply('POST /v1/playlists', { status: 201, json: playlist({ id: 'p9', name: 'Road Trip Mix' }) });
    __http.reply('POST /v1/playlists/p9/tracks/batch', { status: 200, json: { added: 1, skipped: 0 } });
    const { onClose } = renderSheet({ resolveTrackIds: () => Promise.resolve(['t1']) });

    await waitFor(() => screen.getByTestId('add-to-playlist-create-new'));
    fireEvent.press(screen.getByTestId('add-to-playlist-create-new'));
    fireEvent.changeText(screen.getByTestId('create-playlist-input'), 'Road Trip Mix');
    fireEvent.press(screen.getByTestId('create-playlist-confirm'));

    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(screen.queryByTestId('create-playlist-modal')).toBeNull();
  });

  it(
    'a failed playlist creation leaves the create modal open so the user can retry, instead of closing everything (:97-100)',
    async () => {
      __http.reply('GET /v1/playlists', { status: 200, json: { items: [], total: 0 } });
      __http.fail('POST /v1/playlists');
      const { onClose } = renderSheet({ resolveTrackIds: () => Promise.resolve(['t1']) });

      await waitFor(() => screen.getByTestId('add-to-playlist-create-new'));
      fireEvent.press(screen.getByTestId('add-to-playlist-create-new'));
      fireEvent.changeText(screen.getByTestId('create-playlist-input'), 'Road Trip Mix');
      fireEvent.press(screen.getByTestId('create-playlist-confirm'));

      await waitFor(() => expect(alertSpy).toHaveBeenCalled());

      expect(onClose).not.toHaveBeenCalled();
      expect(screen.getByTestId('create-playlist-modal').props.visible).toBe(true);
    },
  );
});

describe('AddToPlaylistSheet(): backing out of the nested create modal returns to the picker (:224)', () => {
  it('cancelling the create modal closes only that modal, leaving the picker open and the sheet never closed', async () => {
    __http.reply('GET /v1/playlists', {
      status: 200,
      json: { items: [playlist({ id: 'p1', name: 'Focus' })], total: 1 },
    });
    const { onClose } = renderSheet();

    await waitFor(() => screen.getByTestId('add-to-playlist-create-new'));
    fireEvent.press(screen.getByTestId('add-to-playlist-create-new'));
    expect(screen.getByTestId('create-playlist-modal')).toBeTruthy();
    expect(screen.queryByTestId('add-to-playlist-sheet')).toBeNull();

    fireEvent.press(
      within(screen.getByTestId('create-playlist-modal')).getByRole('button', { name: 'Cancel' }),
    );

    expect(screen.queryByTestId('create-playlist-modal')).toBeNull();
    expect(screen.getByTestId('add-to-playlist-sheet')).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
    expect(__http.countFor('POST /v1/playlists')).toBe(0);
  });
});
