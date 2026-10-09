import { libraryFailureAlert } from '@features/library/domain/libraryFailureAlert';

describe('libraryFailureAlert — the payload carries the surface, the title as given, the lead then the closing ask', () => {
  it.each([
    ['auth', 'Could not remove the track. Sign in again, then retry.'],
    ['network', 'Could not remove the track. Please try again.'],
    ['server', 'Could not remove the track. Please try again.'],
    ['not-found', 'Could not remove the track. Please try again.'],
    ['unknown', 'Could not remove the track. Please try again.'],
  ] as const)('a %s failure gives "%s"', (failure, message) => {
    expect(
      libraryFailureAlert(
        'library.delete_track',
        'Delete failed',
        'Could not remove the track.',
        failure,
      ),
    ).toEqual({ surface: 'library.delete_track', title: 'Delete failed', message });
  });
});
