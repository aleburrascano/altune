import { Text } from 'react-native';
import { render, screen } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import { clearDetailHandoffs, detailHref } from '@shared/navigation/detailHandoff';

import { useDetailHandoff } from '@/features/detail/hooks/useDetailHandoff';
import { DetailScreen } from '@/features/detail/ui/DetailScreen';

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
type ArtistDiscoveryArgs = { artistName: string; enabled: boolean };
type Resolve = (result: DiscoveryResult) => unknown;
let mockResolution: Resolve | null = null;
jest.mock('@/features/detail/hooks/useResolveMissingSources', () => ({
  useResolveMissingSources: (result: DiscoveryResult) =>
    mockResolution ? mockResolution(result) : { resolved: result, isResolving: false },
}));
const mockArtistDiscovery = jest.fn((_args: ArtistDiscoveryArgs) => ({
  imageUrl: null,
}));
jest.mock('@/features/detail/hooks/useEntityDiscovery', () => ({
  useArtistDiscovery: (args: ArtistDiscoveryArgs) => mockArtistDiscovery(args),
}));
jest.mock('@/features/detail/hooks/useDetailEnrichments', () => ({
  useDetailEnrichments: () => ({
    errors: { musicbrainz: false, deezer: false, lastfm: false },
  }),
}));
jest.mock('@/features/detail/hooks/useDetailNavigation', () => ({ useLateralNav: () => ({}) }));
jest.mock('@/features/detail/ui/secondaryLine', () => ({ secondaryLine: () => null }));

function MockHandoffProbe({ result }: { result: DiscoveryResult }) {
  const handoff = useDetailHandoff();
  return <Text>{`${result.title}|${handoff?.searchId ?? 'none'}`}</Text>;
}
jest.mock('@/features/detail/ui/TrackDetailBody', () => ({
  TrackDetailBody: (props: { result: DiscoveryResult }) => <MockHandoffProbe {...props} />,
}));
jest.mock('@/features/detail/ui/AlbumDetailBody', () => ({ AlbumDetailBody: () => null }));
jest.mock('@/features/detail/ui/ArtistDetailBody', () => ({ ArtistDetailBody: () => null }));

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
  mockResolution = null;
  mockArtistDiscovery.mockClear();
});

describe('DetailScreen on a library artist page', () => {
  const libraryArtist: DiscoveryResult = { ...track('Radiohead'), kind: 'artist', subtitle: null };
  const found = { provider: 'deezer', external_id: '399' } as DiscoveryResult['sources'][number];

  function renderArtist(resolution: (result: DiscoveryResult) => unknown) {
    mockResolution = resolution;
    mockParams = { handoff: detailHref('/discover/detail', libraryArtist).params.handoff };
    render(<DetailScreen />);
    return mockArtistDiscovery.mock.calls.map(([args]) => args.enabled);
  }

  it('does not search again for a photo once the first search found the artist', () => {
    const enabled = renderArtist((result) => ({
      resolved: { ...result, sources: [found] },
      isResolving: false,
    }));

    expect(enabled).not.toContain(true);
  });

  it('does not search again while the first search is still running', () => {
    const enabled = renderArtist((result) => ({ resolved: result, isResolving: true }));

    expect(enabled).not.toContain(true);
  });

  it('searches for a photo when the first search found nothing', () => {
    const enabled = renderArtist((result) => ({ resolved: result, isResolving: false }));

    expect(enabled).toContain(true);
  });
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
      freshDetailHref = require('@shared/navigation/detailHandoff').detailHref;
      FreshDetailScreen = require('@/features/detail/ui/DetailScreen').DetailScreen;
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
