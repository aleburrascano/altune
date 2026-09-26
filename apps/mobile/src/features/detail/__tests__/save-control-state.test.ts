import { asTrackId } from '@shared/api-client/ids';

import type { AcquisitionStatus } from '@shared/api-client/types';

import { ownedTrack, type OwnedTrack } from '../hooks/useOwnedTrack';
import {
  saveControlLabel,
  saveControlState,
  saveControlText,
  type SaveControlState,
} from '../save-control-state';
import { ownedRetryTrackId } from '../save-control-state';
import { saveControlInteractive, saveDisplayState } from '../save-control-state';
import { optimisticTrack, toCreateTrackRequest } from '../save-cache';
import type { DiscoveryResult } from '@shared/api-client/discovery';

function owned(acquisitionStatus: AcquisitionStatus): OwnedTrack {
  return ownedTrack(asTrackId('track-a'), acquisitionStatus, null);
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
      rejected: saveControlText('rejected'),
      add: saveControlText('add'),
    };

    expect(text).toEqual({
      saving: 'Saving…',
      ready: 'Saved',
      failed: 'Retry',
      rejected: "Can't save",
      add: 'Save',
    });
  });
});

describe('ownedRetryTrackId', () => {
  it('returns null for a track that was never owned', () => {
    expect(ownedRetryTrackId(null)).toBeNull();
  });

  it('returns null for an owned track that is not failed', () => {
    expect(ownedRetryTrackId(owned('ready'))).toBeNull();
    expect(ownedRetryTrackId(owned('pending'))).toBeNull();
  });

  it('returns the server trackId for a genuinely owned, failed track', () => {
    const failedTrack = owned('failed');

    expect(ownedRetryTrackId(failedTrack)).toBe(failedTrack.trackId);
  });

  it('returns null for a failed track that only carries an in-flight save placeholder id', () => {
    const result: DiscoveryResult = {
      kind: 'track',
      title: 'Song',
      subtitle: 'Artist',
      image_url: null,
      confidence: 'high',
      sources: [],
      extras: {},
    };
    const placeholderId = optimisticTrack(toCreateTrackRequest(result), '2026-01-01T00:00:00Z').id;
    const placeholder = ownedTrack(placeholderId, 'failed', 'network error');

    expect(ownedRetryTrackId(placeholder)).toBeNull();
  });
});

describe('saveControlInteractive', () => {
  it('lets the user tap save only to add a track or retry a failed one', () => {
    expect(saveControlInteractive('add')).toBe(true);
    expect(saveControlInteractive('failed')).toBe(true);
  });

  it('refuses a tap while disabled, saving, saved or permanently rejected', () => {
    expect(saveControlInteractive('disabled')).toBe(false);
    expect(saveControlInteractive('saving')).toBe(false);
    expect(saveControlInteractive('ready')).toBe(false);
    expect(saveControlInteractive('rejected')).toBe(false);
  });
});

describe('saveDisplayState', () => {
  it('shows a track with no known artist as the plain save affordance', () => {
    expect(saveDisplayState('disabled')).toBe('add');
  });

  it('shows every control state as itself', () => {
    expect(saveDisplayState('add')).toBe('add');
    expect(saveDisplayState('saving')).toBe('saving');
    expect(saveDisplayState('ready')).toBe('ready');
    expect(saveDisplayState('failed')).toBe('failed');
    expect(saveDisplayState('rejected')).toBe('rejected');
  });
});

describe('saveControlLabel for a refused save', () => {
  it('announces that the track could not be saved', () => {
    expect(saveControlLabel('rejected', 'Song')).toBe("Couldn't save Song");
  });
});
