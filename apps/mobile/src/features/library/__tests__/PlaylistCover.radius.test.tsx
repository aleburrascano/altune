import { StyleSheet, type StyleProp, type ViewStyle } from 'react-native';
import { render } from '@testing-library/react-native';

import { radius } from '@shared/ui';

import { PlaylistCover } from '../ui/PlaylistCover';

const ARTWORK = [
  'https://cdn.test/a.jpg',
  'https://cdn.test/b.jpg',
  'https://cdn.test/c.jpg',
  'https://cdn.test/d.jpg',
];

function renderedCornerRadius(artworkUrls: string[]): number | undefined {
  const tree = render(<PlaylistCover artworkUrls={artworkUrls} size={120} />).toJSON();
  const root = Array.isArray(tree) ? tree[0] : tree;
  const style = StyleSheet.flatten(root?.props.style as StyleProp<ViewStyle>);
  return style?.borderRadius as number | undefined;
}

describe('playlist cover corners track the shared radius token, not a loose literal', () => {
  it.each([
    ['placeholder', []],
    ['single cover', ARTWORK.slice(0, 1)],
    ['split cover', ARTWORK.slice(0, 2)],
    ['four-up grid', ARTWORK],
  ])('rounds the %s to radius.sm', (_variant, artworkUrls) => {
    expect(renderedCornerRadius(artworkUrls)).toBe(radius.sm);
  });
});
