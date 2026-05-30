/**
 * DetailScreen — header renders from the in-memory handoff; empty handoff
 * redirects to /discover (view-result-detail slices 11+).
 *
 * expo-image is mocked (Artwork -> expo-image doesn't run under jest) and
 * expo-router is mocked the same way AuthGate's test mocks Redirect.
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import { render } from '@testing-library/react-native';

jest.mock('expo-image', () => ({ Image: () => null }));

const mockBack = jest.fn();
const mockRedirect = jest.fn((_props: { href: string }) => null);
jest.mock('expo-router', () => ({
  useRouter: () => ({ back: mockBack, push: jest.fn(), replace: jest.fn() }),
  Redirect: (props: { href: string }) => mockRedirect(props),
}));

import { clearDetailHandoff, setDetailHandoff } from '@shared/lib/detail-handoff';
import type { DiscoveryResult } from '../../../shared/api-client/discovery';

function _result(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: 'Midnight City',
    subtitle: 'M83',
    image_url: 'https://img.example/mc.jpg',
    confidence: 'high',
    sources: [],
    extras: {},
    ...overrides,
  };
}

afterEach(() => {
  clearDetailHandoff();
  jest.clearAllMocks();
});

describe('DetailScreen', () => {
  it('renders the header from the handoff result', () => {
    setDetailHandoff(_result());
    const { DetailScreen } = require('../ui/DetailScreen');
    const { getByTestId, getByText } = render(<DetailScreen />);
    expect(getByTestId('detail-header')).toBeTruthy();
    expect(getByText('Midnight City')).toBeTruthy();
    expect(mockRedirect).not.toHaveBeenCalled();
  });

  it('redirects to /discover when the handoff is empty', () => {
    clearDetailHandoff();
    const { DetailScreen } = require('../ui/DetailScreen');
    render(<DetailScreen />);
    expect(mockRedirect).toHaveBeenCalledWith({ href: '/discover' });
  });
});
