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

describe('DetailScreen back button', () => {
  function renderCapturingBack(canGoBack: boolean) {
    const back = jest.fn();
    const replace = jest.fn();
    let capturedOnBack: (() => void) | undefined;

    let FreshDetailScreen!: typeof DetailScreen;
    let freshDetailHref!: typeof detailHref;
    jest.resetModules();
    jest.isolateModules(() => {
      jest.doMock('../ui/TrackDetailBody', () => ({
        TrackDetailBody: (props: { chrome: { onBack: () => void } }) => {
          capturedOnBack = props.chrome.onBack;
          return null;
        },
      }));
      jest.doMock('expo-router', () => {
        const { Text: RNText } = jest.requireActual('react-native');
        return {
          useLocalSearchParams: () => mockParams,
          useRouter: () => ({ push: jest.fn(), replace, back, canGoBack: () => canGoBack }),
          useSegments: () => ['(tabs)', 'discover', 'detail'],
          Redirect: ({ href }: { href: string }) => <RNText>{`redirect:${href}`}</RNText>,
        };
      });
      freshDetailHref = require('@shared/lib/detail-handoff').detailHref;
      FreshDetailScreen = require('../ui/DetailScreen').DetailScreen;
    });

    const first = freshDetailHref('/discover/detail', track('First'));
    mockParams = { handoff: first.params.handoff };
    render(<FreshDetailScreen />);

    return { onBack: () => capturedOnBack!(), back, replace };
  }

  it('calls router.back() when canGoBack is true', () => {
    const { onBack, back, replace } = renderCapturingBack(true);

    onBack();

    expect(back).toHaveBeenCalledTimes(1);
    expect(replace).not.toHaveBeenCalled();
  });

  it("calls router.replace('/discover') when canGoBack is false", () => {
    const { onBack, back, replace } = renderCapturingBack(false);

    onBack();

    expect(replace).toHaveBeenCalledWith('/discover');
    expect(back).not.toHaveBeenCalled();
  });
});
