import { featuredArtistsFromExtras } from '@shared/catalog';
import { wireFixture } from '@testing/wireFixtures';

describe('featured artists in the Go extras map', () => {
  it('are read back with the name, mbid and deezer id the server sent', () => {
    const sent = wireFixture('featured_artist_extras') as Record<string, unknown>;

    const [artist] = featuredArtistsFromExtras([sent]);

    expect(artist).toMatchObject({ name: sent['name'], mbid: sent['mbid'] });
    expect(JSON.stringify(artist)).toContain(String(sent['deezer_id']));
  });

  it('are written with exactly the keys the create-track request fixture carries', () => {
    const request = wireFixture('req_tracks_create') as {
      featured_artists: Record<string, unknown>[];
    };
    const sent = wireFixture('featured_artist_extras') as Record<string, unknown>;

    const written = request.featured_artists[0]!;

    expect(Object.keys(written).sort()).toEqual(
      Object.keys(sent)
        .filter((key) => key !== 'role')
        .sort(),
    );
  });
});
