import * as FileSystem from 'expo-file-system';

export interface DownloadProgress {
  bytesWritten: number;
  totalBytes: number;
}

export interface DownloadOptions {
  signal?: AbortSignal;
  onProgress?: (data: DownloadProgress) => void;
}

export interface DownloadTarget {
  uri: string;
}

export interface DownloadedFile {
  uri: string;
}

export type Download = (
  url: string,
  dest: DownloadTarget,
  options?: DownloadOptions,
) => Promise<DownloadedFile>;

export interface DownloadCall {
  url: string;
  uri: string;
}

export interface FileSystemDouble {
  seedFile(uri: string, contents: string): void;
  readFile(uri: string): string | undefined;
  allFiles(): Record<string, string>;
  failNext(kind: 'move' | 'download' | 'delete' | 'list', error?: unknown): void;
  scriptNextDownload(download: Download): void;
  scriptDownloads(download: Download | undefined): void;
  completeDownload: Download;
  downloads(): DownloadCall[];
}

export const fileSystemDouble = (FileSystem as unknown as { __fs: FileSystemDouble }).__fs;
