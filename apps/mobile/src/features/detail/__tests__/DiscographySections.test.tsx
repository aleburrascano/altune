import { render, screen } from '@testing-library/react-native';

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
