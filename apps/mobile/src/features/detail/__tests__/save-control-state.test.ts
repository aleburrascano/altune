import { asTrackId } from '@shared/api-client/ids';

import type { OwnedTrack } from '../hooks/useOwnedTrack';
import {
  saveControlLabel,
  saveControlState,
  saveControlText,
  type SaveControlState,
} from '../save-control-state';

function owned(acquisitionStatus: OwnedTrack['acquisitionStatus']): OwnedTrack {
  return { trackId: asTrackId('track-a'), acquisitionStatus };
}

describe('saveControlState', () => {
  it('derives add for a track with no ownership', () => {
    expect(saveControlState(null)).toBe('add');
  });

  it('derives failed for a failed acquisition', () => {
    expect(saveControlState(owned('failed'))).toBe('failed');
  });

  it('derives saving while acquisition is pending', () => {
    expect(saveControlState(owned('pending'))).toBe('saving');
  });

  it('derives ready once acquisition completes', () => {
    expect(saveControlState(owned('ready'))).toBe('ready');
  });
});

describe('saveControlLabel', () => {
  it('announces the downloading state for a saving track', () => {
    expect(saveControlLabel('saving', 'Song')).toBe('Song downloading');
  });

  it('announces library membership for a ready track', () => {
    expect(saveControlLabel('ready', 'Song')).toBe('Song in library');
  });

  it('offers a retry for a failed track', () => {
    expect(saveControlLabel('failed', 'Song')).toBe('Retry saving Song');
  });

  it('offers to save an unsaved track', () => {
    expect(saveControlLabel('add', 'Song')).toBe('Save Song');
  });
});

describe('saveControlText', () => {
  it('reads distinct labels for each control state', () => {
    const text: Record<SaveControlState, string> = {
      saving: saveControlText('saving'),
      ready: saveControlText('ready'),
      failed: saveControlText('failed'),
      add: saveControlText('add'),
    };

    expect(text).toEqual({
      saving: 'Saving…',
      ready: 'Saved',
      failed: 'Retry',
      add: 'Save',
    });
  });
});
