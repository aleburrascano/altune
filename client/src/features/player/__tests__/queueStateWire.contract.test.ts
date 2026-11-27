import { fromWireSource, parseQueueState, toWireSource } from '@features/player';
import { getQueueState, saveQueueState } from '@shared/apiClient';
import type { QueueStateResponse, SaveQueueStateRequest } from '@shared/apiClient';
import { asPlaylistId } from '@shared/catalog';
import { currentTrackToPlaybackTrack, type QueueSource } from '@shared/playback';
import { signInTestUser } from '@testing/signInTestUser';
import { wireFixture } from '@testing/wireFixtures';

const { __http } = require('@testing/doubles/fetch.js');

const TRACK_DURATION_SECONDS = 233.5;
const QUEUE_STATE_PATH = '/v1/playback/queue-state';
const PLAYLIST_ID = '5d1c3f0e-6f4e-4f55-9a4e-0d6a2c0c9b11';

beforeEach(async () => {
  await signInTestUser();
});

function keysOf(value: unknown): string[] {
  return Object.keys(value as Record<string, unknown>).sort();
}

async function decodedFrom(fixture: string): Promise<QueueStateResponse> {
  __http.reply(`GET ${QUEUE_STATE_PATH}`, { status: 200, json: wireFixture(fixture) });
  const parsed = parseQueueState(await getQueueState());
  if (!parsed.ok) throw parsed.error;
  return parsed.state;
}

describe('GET /v1/playback/queue-state, from the playback_queue_state wire fixtures', () => {
  it.each([
    ['playback_queue_state_playlist', ['source_id']],
    ['playback_queue_state_library', ['source_id']],
    ['playback_queue_state_search', ['source_id']],
    ['playback_queue_state_nosource', ['source_id']],
    ['playback_queue_state_track_bare', ['source_id']],
    ['playback_queue_state_track_unavailable', ['current_track_unavailable', 'source_id']],
  ])('%s decodes, and the client drops only %j', async (fixture, dropped) => {
    const state = await decodedFrom(fixture);
    const sent = keysOf(wireFixture(fixture));
    const kept = keysOf(state);

    expect(kept.filter((key) => !sent.includes(key))).toEqual([]);
    expect(sent.filter((key) => !kept.includes(key))).toEqual(dropped);
  });

  it('carries the current track the server sends through to a playback track', async () => {
    const state = await decodedFrom('playback_queue_state_playlist');

    const track = currentTrackToPlaybackTrack(state.current_track!);

    expect(track.artworkUrl).toBe('https://cdn.example.com/art/ok-computer.jpg');
    expect(track.durationSeconds).toBe(TRACK_DURATION_SECONDS);
    expect(track.title).toBe('Paranoid Android');
  });

  it('reads a current track with a null artwork and duration without inventing either', async () => {
    const state = await decodedFrom('playback_queue_state_track_bare');

    const track = currentTrackToPlaybackTrack(state.current_track!);

    expect(state.current_track?.artwork_url).toBeNull();
    expect(state.current_track?.duration_seconds).toBeNull();
    expect(track.artworkUrl).toBeNull();
    expect(track.durationSeconds).toBeUndefined();
  });

  it.each([
    ['playback_queue_state_playlist', 'playlist'],
    ['playback_queue_state_library', 'library'],
    ['playback_queue_state_search', 'search'],
  ])('%s carries a source the client reads as kind %s', async (fixture, kind) => {
    const state = await decodedFrom(fixture);

    expect(state.source?.kind).toBe(kind);
    expect(fromWireSource(state.source)?.kind).toBe(kind);
  });

  it('decodes a queue with no source as null', async () => {
    const state = await decodedFrom('playback_queue_state_nosource');

    expect(state.source).toBeNull();
  });
});

describe('the queue source kinds, from the playback_queue_source_kinds wire fixture', () => {
  it('are exactly the kinds of the QueueSource union', () => {
    const everyKind: Record<QueueSource['kind'], QueueSource> = {
      library: { kind: 'library' },
      playlist: { kind: 'playlist', playlistId: asPlaylistId(PLAYLIST_ID), name: 'Road trip' },
      search: { kind: 'search', query: 'radiohead' },
    };

    const clientKinds = Object.values(everyKind).map((source) => toWireSource(source)?.kind);

    expect([...(clientKinds as string[])].sort()).toEqual(
      [...(wireFixture('playback_queue_source_kinds') as string[])].sort(),
    );
  });
});

describe('PUT /v1/playback/queue-state, against the req_playback_queue_state wire fixture', () => {
  const request: SaveQueueStateRequest = {
    track_ids: ['00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002'],
    current_index: 1,
    position_ms: 42000,
    shuffled: true,
    repeat_mode: 'all',
    source: toWireSource({
      kind: 'playlist',
      playlistId: asPlaylistId(PLAYLIST_ID),
      name: 'Road trip',
    }),
    natural_order: ['00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001'],
  };

  async function sentBody(): Promise<Record<string, unknown>> {
    __http.reply(`PUT ${QUEUE_STATE_PATH}`, { status: 204 });
    await saveQueueState(request);
    return JSON.parse(__http.last().body as string) as Record<string, unknown>;
  }

  it('sends exactly the fields the server accepts, and the same source fields', async () => {
    const fixture = wireFixture('req_playback_queue_state') as Record<string, unknown>;
    const body = await sentBody();

    expect(keysOf(body)).toEqual(keysOf(fixture));
    expect(keysOf(body.source)).toEqual(keysOf(fixture.source));
  });
});
