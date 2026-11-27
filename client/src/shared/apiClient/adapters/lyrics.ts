import { apiFetch, signalInit, withQuery } from '@shared/apiClient/adapters/http';
import { asNumber, asRecord, asString, parseArray } from '@shared/apiClient/core/wireDecoders';

export type SyncedLine = {
  timecode: string;
  line: string;
  milliseconds: number;
  duration: number;
};

export type LyricsResponse = {
  plain: string;
  synced_lines: SyncedLine[];
  writers: string[];
  copyright: string;
};

function parseSyncedLine(value: unknown, at: string): SyncedLine {
  const r = asRecord(value, at);
  return {
    timecode: asString(r.timecode, `${at}.timecode`),
    line: asString(r.line, `${at}.line`),
    milliseconds: asNumber(r.milliseconds, `${at}.milliseconds`),
    duration: asNumber(r.duration, `${at}.duration`),
  };
}

function parseLyricsResponse(value: unknown, at = 'LyricsResponse'): LyricsResponse {
  const r = asRecord(value, at);
  return {
    plain: asString(r.plain, `${at}.plain`),
    synced_lines:
      r.synced_lines == null
        ? []
        : parseArray(r.synced_lines, `${at}.synced_lines`, parseSyncedLine),
    writers: r.writers == null ? [] : parseArray(r.writers, `${at}.writers`, asString),
    copyright: asString(r.copyright, `${at}.copyright`),
  };
}

type LyricsParams = {
  title: string;
  subtitle?: string | null | undefined;
};

export async function getLyrics(
  params: LyricsParams,
  signal?: AbortSignal,
): Promise<LyricsResponse> {
  const qs = new URLSearchParams({ title: params.title });
  if (params.subtitle != null && params.subtitle.length > 0) {
    qs.set('subtitle', params.subtitle);
  }
  return parseLyricsResponse(
    await apiFetch(withQuery('/v1/discovery/lyrics', qs), signalInit(signal)),
  );
}
