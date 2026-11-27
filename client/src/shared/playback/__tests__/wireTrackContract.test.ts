import { parseTrackResponse } from '@shared/apiClient';
import { toPlaybackTrack } from '@shared/playback';
import { wireFixture } from '@testing/wireFixtures';

describe('a track from go-api becomes a playable track', () => {
  it('carries everything the player reads from the wire fixture of the track listing', () => {
    const items = (wireFixture('tracks_list') as { items: unknown[] }).items;
    const track = parseTrackResponse(items[0]);

    const playable = toPlaybackTrack(track);

    expect(playable.source).toEqual({ kind: 'library', trackId: track.id });
    expect(playable.title).toBe(track.title);
    expect(playable.artist).toBe(track.artist);
    expect(playable.artworkUrl).toBe(track.artwork_url);
    expect(typeof playable.durationSeconds).toBe('number');
    expect(playable.featuredArtists?.[0]?.name).toBe('Guest');
  });
});
