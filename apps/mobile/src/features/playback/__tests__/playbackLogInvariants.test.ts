import { readdirSync, readFileSync } from 'fs';
import { join } from 'path';
import { inspect } from 'util';

import { Event } from 'react-native-track-player';

import { asTrackId } from '@shared/api-client/ids';
import { recordEvent } from '@shared/telemetry/recordEvent';

import { ignoringNativeRejection } from '@features/playback/native/createNativePlaybackActions';
import { playbackService } from '@features/playback/native/service';
import { repairActiveToStreaming } from '@features/playback/native/nativeTrackSwap';
import { _resetPlaybackHealthForTest } from '@features/playback/domain/playbackHealth';
import { reportingQueueFailure } from '@features/playback/native/queueFailureReport';
import { redactPlaybackErrorMessage, warnPlayback } from '@features/playback/domain/redactPlaybackError';

import { installPlaybackPort } from '@features/playback/ports/activePlaybackPort';

import { libraryTrack } from '@features/playback/__tests__/fixtures';
import { createMemoryPlaybackPort } from '@features/playback/__tests__/memoryPlaybackPort';

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
const leaky = () =>
  new Error(
    'GET https://cdn.example/a.mp3?X-Amz-Signature=deadbeef failed, Authorization: Bearer abc.def.ghi',
  );
let warn: jest.SpyInstance;
let restorePort: (() => void) | null = null;

function useFailingPresign(error: Error): void {
  restorePort?.();
  restorePort = installPlaybackPort({
    ...createMemoryPlaybackPort(),
    fetchAudioUrls: () => Promise.reject(error),
  });
}

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
  restorePort?.();
  restorePort = null;
});

describe('playback logs redact rejections', () => {
  const track = () => libraryTrack({ source: { kind: 'library', trackId: asTrackId('t1') } });

  it('swap-path presign failure', async () => {
    useFailingPresign(leaky());
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

describe('warnPlayback', () => {
  it('prefixes the message and puts the redacted failure under error beside the context', () => {
    warnPlayback('demo failed', { stage: 'x' }, leaky());
    expect(warn).toHaveBeenCalledWith('[playback] demo failed', {
      stage: 'x',
      error: { kind: expect.any(String), message: expect.not.stringMatching(/deadbeef|abc\.def/) },
    });
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

describe('unmapped native playback error log', () => {
  it('redacts a tokenized URL in the native message', async () => {
    const { __player } = jest.requireMock('react-native-track-player');
    await playbackService();
    const registration = __player
      .calls('addEventListener')
      .find(([event]: [unknown]) => event === Event.PlaybackError);
    registration[1]({
      code: 'android-audio-track-init-failed',
      message: leaky().message,
    });
    await new Promise((resolve) => setImmediate(resolve));
    expectLoggedClean();
  });
});

describe('redactPlaybackErrorMessage truncation boundary', () => {
  it('leaves no JWT fragment when the 500-char cut falls before its second dot', () => {
    const jwt = 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJl';
    const message = `${'x '.repeat(236)}${jwt}`;
    expect(message.indexOf('.', message.indexOf('.') + 1)).toBeGreaterThan(500);
    const result = redactPlaybackErrorMessage(message);
    expect(result).not.toContain('eyJ');
    expect(result.length).toBeLessThanOrEqual(500);
  });
});
