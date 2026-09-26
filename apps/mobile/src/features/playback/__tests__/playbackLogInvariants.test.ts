// Regression for #2526: a native or presign rejection can carry a signed stream URL or a bearer
// token, so no playback console.warn may log a raw error. Behaviour is checked at the swap and
// command sites; a source guard fails on any raw caught error handed to console.warn.

import { readdirSync, readFileSync } from 'fs';
import { join } from 'path';
import { inspect } from 'util';

import { fetchAudioUrls } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import { recordEvent } from '@shared/telemetry/recordEvent';

import { ignoringNativeRejection } from '../createNativePlaybackActions';
import { repairActiveToStreaming } from '../nativeTrackSwap';
import { _resetPlaybackHealthForTest } from '../playbackHealth';
import { reportingQueueFailure } from '../queueFailureReport';

import { libraryTrack } from './fixtures';

jest.mock('@shared/telemetry/recordEvent', () => ({
  recordEvent: jest.fn().mockResolvedValue(undefined),
}));
jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest
        .fn()
        .mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null }),
    },
  },
}));
jest.mock('@shared/api-client/audio', () => ({
  ...jest.requireActual('@shared/api-client/audio'),
  fetchAudioUrls: jest.fn(),
}));

const leaky = () =>
  new Error(
    'GET https://cdn.example/a.mp3?X-Amz-Signature=deadbeef failed, Authorization: Bearer abc.def.ghi',
  );
const fetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;
let warn: jest.SpyInstance;

function expectLoggedClean(): void {
  expect(warn).toHaveBeenCalled();
  expect(inspect(warn.mock.calls, { depth: 6 })).not.toMatch(
    /deadbeef|X-Amz|abc\.def|cdn\.example/,
  );
}

beforeEach(() => {
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  _resetPlaybackHealthForTest();
  (recordEvent as jest.Mock).mockClear();
});

afterEach(() => {
  warn.mockRestore();
});

describe('playback logs redact rejections', () => {
  const track = () => libraryTrack({ source: { kind: 'library', trackId: asTrackId('t1') } });

  it('swap-path presign failure', async () => {
    fetchUrls.mockRejectedValue(leaky());
    await repairActiveToStreaming(track());
    expectLoggedClean();
  });

  it('ignored native command rejection', async () => {
    await ignoringNativeRejection(() => Promise.reject(leaky()));
    expectLoggedClean();
  });

  it('native queue mutation failure', async () => {
    await reportingQueueFailure(
      () => null,
      'add',
      () => Promise.reject(leaky()),
    );
    expectLoggedClean();
  });
});

describe('source guard', () => {
  const root = join(__dirname, '..');
  const files = [root, join(root, 'hooks')].flatMap((dir) =>
    readdirSync(dir)
      .filter((f) => /\.tsx?$/.test(f))
      .map((f) => join(dir, f)),
  );

  it('never passes a raw caught error to console.warn', () => {
    const offenders: string[] = [];
    for (const file of files) {
      const src = readFileSync(file, 'utf8');
      for (const m of src.matchAll(/console\.warn\(([\s\S]*?)\);/g)) {
        if (/error:\s*(err|e|error)\b|,\s*(err|e|error)\s*,?\s*$/.test(m[1]!))
          offenders.push(`${file}: ${m[0].slice(0, 80)}`);
      }
    }
    expect(offenders).toEqual([]);
  });
});
