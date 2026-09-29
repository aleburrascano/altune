import { QueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import { showAlert } from '@shared/ui/dialog/dialog';

import { dropVanishedTrack } from '../hooks/dropVanishedTrack';

jest.mock('@shared/ui/dialog/dialog', () => ({ showAlert: jest.fn() }));

describe('dropVanishedTrack', () => {
  it('tells the user through the dialog port that the track is gone', () => {
    dropVanishedTrack(new QueryClient(), 'track-1' as TrackId);

    expect(jest.mocked(showAlert).mock.calls).toEqual([
      ['Track not found', 'This track is no longer in your library.'],
    ]);
  });
});
