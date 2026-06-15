import { formatFailureReason } from '../ui/formatFailureReason';

describe('formatFailureReason', () => {
  it('maps no_match_found to a readable message', () => {
    expect(formatFailureReason('no_match_found')).toBe("Couldn't find this track");
  });

  it('maps download_failed', () => {
    expect(formatFailureReason('download_failed')).toBe('Download failed');
  });

  it('maps ytdlp_error', () => {
    expect(formatFailureReason('ytdlp_error')).toBe('Download error');
  });

  it('returns fallback for null', () => {
    expect(formatFailureReason(null)).toBe('Acquisition failed');
  });

  it('returns generic fallback for unknown reason', () => {
    expect(formatFailureReason('some_unknown_error')).toBe("Couldn't get this track");
  });
});
