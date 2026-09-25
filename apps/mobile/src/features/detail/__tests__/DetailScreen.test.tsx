// #930: the detail screen resolves the tapped result from its own route param,
// so a second navigation can never change what an earlier screen (or the hooks
// under it) reads.

import { Text } from 'react-native';
import { render, screen } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { clearDetailHandoffs, detailHref } from '@shared/lib/detail-handoff';

import { useDetailHandoff } from '../handoff-context';
import { DetailScreen } from '../ui/DetailScreen';

let mockParams: { handoff?: string } = {};
jest.mock('expo-router', () => {
  const { Text: RNText } = jest.requireActual('react-native');
  return {
    useLocalSearchParams: () => mockParams,
    useRouter: () => ({
      push: jest.fn(),
      replace: jest.fn(),
      back: jest.fn(),
      canGoBack: () => true,
    }),
    useSegments: () => ['(tabs)', 'discover', 'detail'],
    Redirect: ({ href }: { href: string }) => <RNText>{`redirect:${href}`}</RNText>,
  };
});
jest.mock('../hooks/useResolveMissingSources', () => ({
  useResolveMissingSources: (result: unknown) => ({ resolved: result }),
}));
jest.mock('../hooks/useArtistDiscovery', () => ({
  useArtistDiscovery: () => ({ imageUrl: null }),
}));
jest.mock('../hooks/useDetailEnrichments', () => ({
  useDetailEnrichments: () => ({
    errors: { musicbrainz: false, deezer: false, lastfm: false },
  }),
}));
jest.mock('../hooks/useLateralNav', () => ({ useLateralNav: () => ({}) }));
jest.mock('../ui/secondaryLine', () => ({ secondaryLine: () => null }));

function MockHandoffProbe({ result }: { result: DiscoveryResult }) {
  const handoff = useDetailHandoff();
  return <Text>{`${result.title}|${handoff?.searchId ?? 'none'}`}</Text>;
}
jest.mock('../ui/TrackDetailBody', () => ({
  TrackDetailBody: (props: { result: DiscoveryResult }) => <MockHandoffProbe {...props} />,
}));
jest.mock('../ui/AlbumDetailBody', () => ({ AlbumDetailBody: () => null }));
jest.mock('../ui/ArtistDetailBody', () => ({ ArtistDetailBody: () => null }));

function track(title: string): DiscoveryResult {
  return {
    kind: 'track',
    title,
    subtitle: 'Artist',
    image_url: null,
    confidence: 'high',
    sources: [],
    extras: {},
  };
}

beforeEach(() => {
  clearDetailHandoffs();
  mockParams = {};
});

describe('DetailScreen reads the handoff named by its route param', () => {
  it('renders the result and search id of its own navigation after a later tap', () => {
    const first = detailHref('/discover/detail', track('First'), 'search-1');
    detailHref('/discover/detail', track('Second'), 'search-2');

    mockParams = { handoff: first.params.handoff };
    render(<DetailScreen />);

    expect(screen.getByText('First|search-1')).toBeTruthy();
  });

  it('redirects to discover when the param names no handoff', () => {
    mockParams = { handoff: 'unknown' };
    render(<DetailScreen />);

    expect(screen.getByText('redirect:/discover')).toBeTruthy();
  });
});
