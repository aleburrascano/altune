// Regression for #828: POST /v1/audio-urls is the only per-call check that the caller may
// still stream a track. A failed presign used to fall back to the pinned (downloaded) file
// whatever the failure, so a 401/403 (access revoked, session rejected) still played the
// previously downloaded bytes. The distinction the load now draws:
// - authorization denied (401/403): never serve the pinned file; the track streams, and the
//   stream endpoint re-checks authorization itself.
// - network/offline (transport failure, timeout) or a server fault: the pinned file still
//   plays, so downloads keep working while genuinely offline.

import { asTrackId } from '@shared/api-client/ids';
import { usePinnedStore } from '@shared/offline/pinnedStore';

import { appendNativeTrack, loadNativeQueue, loadNativeTrack } from '../loadNativeTrack';

import { libraryTrack } from './fixtures';

const { __player } = jest.requireMock('react-native-track-player');
const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest
        .fn()
        .mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null }),
    },
  },
}));

const PINNED_URI = 'file:///document/offline-audio/t1.mp3';
const TRACK = libraryTrack({ source: { kind: 'library', trackId: asTrackId('t1') } });

// Only a ready entry records the version it downloaded, so the read narrows on status first.
function pinnedVersion(trackId: string): string | undefined {
  const entry = usePinnedStore.getState().entries[trackId];
  return entry?.status === 'ready' ? entry.version : undefined;
}

function addedUrls(): string[] {
  return (__player.calls('add') as unknown[][]).flatMap(([arg]) =>
    (Array.isArray(arg) ? arg : [arg]).map((t: { url: string }) => t.url),
  );
}

beforeEach(() => {
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  usePinnedStore.setState({
    entries: { t1: { trackId: asTrackId('t1'), status: 'ready', uri: PINNED_URI, version: 'v1' } },
    queue: [],
    isWorking: false,
  });
});

describe('presign failure and pinned audio (#828)', () => {
  it.each([401, 403])(
    'does not serve the pinned file when presign is denied with %i',
    async (status) => {
      __http.reply('POST /v1/audio-urls', { status, json: { code: 'forbidden' } });

      await loadNativeTrack(TRACK, { autoplay: false });

      expect(addedUrls()).toHaveLength(1);
      expect(addedUrls()).not.toContain(PINNED_URI);
      expect(addedUrls()[0]).toMatch(/\/v1\/tracks\/t1\/audio$/);
    },
  );

  it('does not serve the pinned file to a queue load or append when denied', async () => {
    __http.reply('POST /v1/audio-urls', { status: 403 });

    await loadNativeQueue([TRACK], 0, { autoplay: false });
    await appendNativeTrack(TRACK);

    expect(addedUrls()).toHaveLength(2);
    expect(addedUrls()).not.toContain(PINNED_URI);
  });

  it('still serves the pinned file when presign fails because the device is offline', async () => {
    __http.fail('POST /v1/audio-urls');

    await loadNativeTrack(TRACK, { autoplay: false });

    expect(addedUrls()).toEqual([PINNED_URI]);
  });

  it('still serves the pinned file when the server faults rather than denies', async () => {
    __http.reply('POST /v1/audio-urls', { status: 503 });

    await loadNativeTrack(TRACK, { autoplay: false });

    expect(addedUrls()).toEqual([PINNED_URI]);
  });

  it('serves the pinned file when presign succeeds with a matching version', async () => {
    __http.reply('POST /v1/audio-urls', {
      json: { urls: [{ track_id: 't1', url: 'https://signed.example/t1', version: 'v1' }] },
    });

    await loadNativeTrack(TRACK, { autoplay: false });

    expect(addedUrls()).toEqual([PINNED_URI]);
  });

  it('streams instead of the pinned file, and drops the stale copy, when the server has moved on a version', async () => {
    __http.reply('POST /v1/audio-urls', {
      json: { urls: [{ track_id: 't1', url: 'https://signed.example/t1', version: 'v2' }] },
    });

    await loadNativeTrack(TRACK, { autoplay: false });

    expect(addedUrls()).toEqual(['https://signed.example/t1']);
    expect(pinnedVersion('t1')).not.toBe('v1');
  });
});
