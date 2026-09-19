import * as FileSystem from 'expo-file-system';

import { runSignOutCleanups } from '@shared/session/signOutCleanup';

import { claimPinnedDownloads, resolvePinnedUri, usePinnedStore } from '../pinnedStore';
import { asTrackId } from '@shared/api-client/ids';

jest.mock('@shared/api-client/audio', () => ({
  fetchAudioUrls: jest.fn().mockResolvedValue([]),
}));

const { __fs } = FileSystem as unknown as {
  __fs: {
    seedFile(uri: string, contents: string): void;
    readFile(uri: string): string | undefined;
    failNext(kind: 'read' | 'delete', error?: Error): void;
  };
};

const OWNER_URI = 'file:///document/offline/pinned-owner';
const AUDIO_URI = 'file:///document/offline-audio/t1.mp3';

function seedReadyDownload(): void {
  __fs.seedFile(AUDIO_URI, 'audio-bytes');
  usePinnedStore.setState({
    entries: { t1: { trackId: asTrackId('t1'), status: 'ready', uri: AUDIO_URI } },
    queue: [],
    isWorking: false,
  });
}

beforeEach(() => {
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
});

describe('claimPinnedDownloads — downloads belong to the account that made them (#835)', () => {
  it('keeps downloads the claiming user already owns', () => {
    __fs.seedFile(OWNER_URI, 'user-a');
    seedReadyDownload();

    claimPinnedDownloads('user-a');

    expect(resolvePinnedUri(asTrackId('t1'))).toBe(AUDIO_URI);
    expect(__fs.readFile(AUDIO_URI)).toBe('audio-bytes');
  });

  it('deletes another account downloads and records the new owner', () => {
    __fs.seedFile(OWNER_URI, 'user-a');
    seedReadyDownload();

    claimPinnedDownloads('user-b');

    expect(resolvePinnedUri(asTrackId('t1'))).toBeUndefined();
    expect(__fs.readFile(AUDIO_URI)).toBeUndefined();
    expect(__fs.readFile(OWNER_URI)).toBe('user-b');
  });

  it('never adopts another account download whose file failed to delete (#837)', () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    __fs.seedFile(OWNER_URI, 'user-a');
    seedReadyDownload();
    __fs.failNext('delete', new Error('file is locked'));

    claimPinnedDownloads('user-b');

    expect(resolvePinnedUri(asTrackId('t1'))).toBeUndefined();
    expect(usePinnedStore.getState().entries).toEqual({});
    expect(__fs.readFile(OWNER_URI)).toBe('user-b');
    warn.mockRestore();
  });

  it('treats an unreadable owner record as foreign and deletes the downloads', () => {
    __fs.seedFile(OWNER_URI, 'user-a');
    seedReadyDownload();
    __fs.failNext('read');

    claimPinnedDownloads('user-a');

    expect(resolvePinnedUri(asTrackId('t1'))).toBeUndefined();
    expect(__fs.readFile(AUDIO_URI)).toBeUndefined();
  });

  it('a failed owner write leaves no owner on disk, so the next claim clears again instead of adopting', () => {
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    const realWrite = FileSystem.File.prototype.write;
    const write = jest.spyOn(FileSystem.File.prototype, 'write').mockImplementation(function (
      this: FileSystem.File,
      ...args
    ) {
      if (this.uri === OWNER_URI) throw new Error('ENOSPC');
      return realWrite.apply(this, args);
    });
    seedReadyDownload();

    claimPinnedDownloads('user-a');

    expect(__fs.readFile(OWNER_URI)).toBeUndefined();
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('pinned owner'));
    write.mockRestore();
    warn.mockRestore();
  });
});

describe('sign-out cleanup registry', () => {
  it('removes every download when the signed-in identity changes', () => {
    seedReadyDownload();

    runSignOutCleanups();

    expect(usePinnedStore.getState().entries).toEqual({});
    expect(__fs.readFile(AUDIO_URI)).toBeUndefined();
  });
});
