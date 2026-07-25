import { trackExtras } from '../extras-accessors';
import { resolvePlaySource } from '../play-source';
import type { OwnedTrack } from '../hooks/useOwnedTrack';

function _match(overrides: Partial<OwnedTrack> = {}): OwnedTrack {
  return { trackId: 'lib-1', acquisitionStatus: 'ready', ...overrides };
}

describe('resolvePlaySource', () => {
  it('resolves the library source from extras when acquired', () => {
    const te = trackExtras({ track_id: 'trk-9', acquisition_status: 'ready' });
    expect(resolvePlaySource(te, null)).toEqual({ kind: 'library', trackId: 'trk-9' });
  });

  it('resolves the library source from the live library match', () => {
    const te = trackExtras({});
    expect(resolvePlaySource(te, _match())).toEqual({ kind: 'library', trackId: 'lib-1' });
  });

  it('prefers the extras identity over the match', () => {
    const te = trackExtras({ track_id: 'trk-9', acquisition_status: 'ready' });
    expect(resolvePlaySource(te, _match())).toEqual({ kind: 'library', trackId: 'trk-9' });
  });

  it('falls back to the preview while acquisition is pending', () => {
    const te = trackExtras({ preview_url: 'https://p.mp3' });
    expect(resolvePlaySource(te, _match({ acquisitionStatus: 'pending' }))).toEqual({
      kind: 'preview',
      previewUrl: 'https://p.mp3',
    });
  });

  it('falls back to the preview when not in the library', () => {
    const te = trackExtras({ preview_url: 'https://p.mp3' });
    expect(resolvePlaySource(te, null)).toEqual({ kind: 'preview', previewUrl: 'https://p.mp3' });
  });

  it('resolves to null with no playable source', () => {
    expect(resolvePlaySource(trackExtras({}), null)).toBeNull();
    expect(resolvePlaySource(trackExtras({}), _match({ acquisitionStatus: 'failed' }))).toBeNull();
  });
});
