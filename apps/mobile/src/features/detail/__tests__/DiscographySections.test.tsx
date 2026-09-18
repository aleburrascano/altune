import { fireEvent, render, screen } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { DiscographySections } from '../ui/DiscographySections';

function album(extras: Record<string, unknown>): DiscoveryResult {
  return {
    kind: 'album',
    title: 'Zero Tracks',
    subtitle: null,
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'test', external_id: 'ext-1', url: 'https://altune.test/a1' }],
    extras,
  };
}

// A locally-saved album: no provider source, which is the case that reaches this
// rail through "Your albums" (#1668).
function savedAlbum(title: string): DiscoveryResult {
  return {
    kind: 'album',
    title,
    subtitle: 'Prolific Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: { record_type: 'album', track_count: 3 },
  };
}

describe('DiscographySections(): a track_count of 0 is announced exactly as it is shown', () => {
  it('shows "0 tracks" and announces it in the accessibility label', () => {
    render(
      <DiscographySections albums={[album({ record_type: 'album', track_count: 0 })]} onAlbumPress={jest.fn()} />,
    );

    // The visible caption prints "0 tracks".
    expect(screen.getByText('0 tracks')).toBeTruthy();

    // The accessibility label must announce the same count it shows.
    const label = screen.getByTestId('detail-album-0').props.accessibilityLabel;
    expect(label).toContain('0 tracks');
  });
});

describe('DiscographySections(): "See all" on a discography longer than the rail window', () => {
  it('mounts a window of cards rather than one card per album', () => {
    const albums = Array.from({ length: 300 }, (_unused, i) => savedAlbum(`Album ${i}`));
    render(<DiscographySections albums={albums} onAlbumPress={jest.fn()} />);

    fireEvent.press(screen.getByTestId('detail-see-all-album'));

    // Expanding must not turn the whole list into one synchronous mount: the rail
    // renders its first window and reaches the rest as the user scrolls.
    const cards = screen.queryAllByTestId(/^detail-album-\d+$/);
    expect(cards.length).toBeGreaterThan(0);
    expect(cards.length).toBeLessThanOrEqual(10);
  });
});
