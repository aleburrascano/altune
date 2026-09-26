import { StyleSheet, Text } from 'react-native';
import { render, screen } from '@testing-library/react-native';
import type { ReactTestRendererJSON } from 'react-test-renderer';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { AlbumCardsSkeleton } from '../ui/DetailSkeleton';
import { DetailScaffold } from '../ui/DetailScaffold';
import { DiscographySections } from '../ui/DiscographySections';
import { RelatedTracksSection } from '../ui/RelatedTracksSection';
import { gridCellWidthFor } from '../ui/layout';

jest.mock('expo-router', () => ({ useRouter: () => ({ push: jest.fn() }) }));

jest.mock('../hooks/useRelatedTracks', () => ({
  useRelatedTracks: () => ({
    relatedTracks: [
      {
        kind: 'track',
        title: 'Track A',
        subtitle: null,
        image_url: null,
        confidence: 'high',
        sources: [{ provider: 'test', external_id: 'Track A', url: 'https://altune.test/Track A' }],
        extras: {},
      },
      {
        kind: 'track',
        title: 'Track B',
        subtitle: null,
        image_url: null,
        confidence: 'high',
        sources: [{ provider: 'test', external_id: 'Track B', url: 'https://altune.test/Track B' }],
        extras: {},
      },
    ],
    isLoading: false,
    isError: false,
    failure: null,
  }),
}));

function album(title: string, extras: Record<string, unknown> = { record_type: 'album' }): DiscoveryResult {
  return {
    kind: 'album',
    title,
    subtitle: null,
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'test', external_id: title, url: `https://altune.test/${title}` }],
    extras,
  };
}

function styleOf(testID: string): Record<string, unknown> {
  return (StyleSheet.flatten(screen.getByTestId(testID).props.style) as Record<string, unknown>) ?? {};
}

function widthsInSubtree(node: ReactTestRendererJSON): number[] {
  const style = StyleSheet.flatten(node.props?.style) as Record<string, unknown> | undefined;
  const own = typeof style?.width === 'number' ? [style.width as number] : [];
  const children = (node.children ?? []).filter(
    (child): child is ReactTestRendererJSON => typeof child === 'object' && child !== null,
  );
  return own.concat(...children.map(widthsInSubtree));
}

function findByTestId(
  node: ReactTestRendererJSON | ReactTestRendererJSON[] | null,
  testID: string,
): ReactTestRendererJSON | null {
  if (node == null) {
    return null;
  }
  if (Array.isArray(node)) {
    for (const child of node) {
      const found = findByTestId(child, testID);
      if (found != null) {
        return found;
      }
    }
    return null;
  }
  if (node.props?.testID === testID) {
    return node;
  }
  const children = (node.children ?? []).filter(
    (child): child is ReactTestRendererJSON => typeof child === 'object' && child !== null,
  );
  for (const child of children) {
    const found = findByTestId(child, testID);
    if (found != null) {
      return found;
    }
  }
  return null;
}

describe('detail layout: current rendered widths and margins stay pinned', () => {
  it('keeps the scaffold body gutter at 16px', () => {
    render(
      <DetailScaffold title="A Title" artworkUrl={null} onBack={jest.fn()} actions={null}>
        <Text>content</Text>
      </DetailScaffold>,
    );

    expect(styleOf('detail-body').paddingHorizontal).toBe(16);
  });

  it('keeps the discography rail bleeding edge-to-edge by the gutter, with its content padded back in', () => {
    render(<DiscographySections albums={[album('Album One')]} onAlbumPress={jest.fn()} />);

    const rail = styleOf('detail-discography-rail');
    expect(rail.marginHorizontal).toBe(-16);
  });

  it('keeps discography cards at 128px wide', () => {
    render(<DiscographySections albums={[album('Album One')]} onAlbumPress={jest.fn()} />);

    expect(styleOf('detail-album-0').width).toBe(128);
  });

  it('keeps the discography see-all card at 128 by 128', () => {
    const albums = Array.from({ length: 12 }, (_unused, i) => album(`Album ${i}`));
    render(<DiscographySections albums={albums} onAlbumPress={jest.fn()} />);

    const seeAll = styleOf('detail-see-all-album');
    expect(seeAll.width).toBe(128);
    expect(seeAll.height).toBe(128);
  });

  it('keeps the discography skeleton cards at 130px', () => {
    render(<AlbumCardsSkeleton />);

    const skeletonRail = findByTestId(screen.toJSON(), 'detail-discography-skeleton');
    expect(widthsInSubtree(skeletonRail!)).toContain(130);
  });

  it('keeps related-tracks cards at 132px wide', () => {
    render(
      <RelatedTracksSection result={album('Seed Album')} detailRoute="/discover/detail" />,
    );

    expect(styleOf('detail-related-0').width).toBe(132);
  });
});

describe('gridCellWidthFor(): never returns a negative or zero card width', () => {
  it('clamps to a positive width when the measured container is too narrow for its columns', () => {
    expect(gridCellWidthFor(20, 4)).toBeGreaterThan(0);
    expect(gridCellWidthFor(0, 4)).toBeGreaterThan(0);
    expect(gridCellWidthFor(35, 4)).toBeGreaterThan(0);
  });
});
