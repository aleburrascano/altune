/**
 * DeezerEnrichmentSection — the Deezer metadata block (docs/providers/deezer.md caps 7–8).
 *
 * Pure presentational: track tempo + explicit badge, album label + genre pills.
 * useTheme defaults to the dark theme without a provider, so it renders bare.
 */
import { render } from '@testing-library/react-native';
import { createElement } from 'react';

import { DeezerEnrichmentSection } from '../ui/DeezerEnrichmentSection';
import type {
  DeezerEnrichmentResponse,
  DiscoveryKind,
} from '../../../shared/api-client/discovery';

function _enrichment(over: Partial<DeezerEnrichmentResponse> = {}): DeezerEnrichmentResponse {
  return {
    bpm: 172,
    gain: -8.3,
    explicit: true,
    label: 'Daft Life Ltd./ADA France',
    genres: ['Electro', 'Dance'],
    upc: '724384960650',
    record_type: 'album',
    ...over,
  };
}

function _render(kind: DiscoveryKind, enrichment: DeezerEnrichmentResponse | null) {
  return render(createElement(DeezerEnrichmentSection, { kind, enrichment }));
}

describe('DeezerEnrichmentSection', () => {
  it('renders tempo and explicit badge for a track', () => {
    const { getByTestId } = _render('track', _enrichment());
    expect(getByTestId('detail-deezer')).toBeTruthy();
    expect(getByTestId('detail-deezer-meta')).toHaveTextContent(/172 BPM/);
    expect(getByTestId('detail-deezer-meta')).toHaveTextContent(/Explicit/);
  });

  it('does not render album fields for a track', () => {
    const { queryByTestId } = _render('track', _enrichment());
    expect(queryByTestId('detail-deezer-label')).toBeNull();
    expect(queryByTestId('detail-deezer-genre-0')).toBeNull();
  });

  it('renders label and genre pills for an album', () => {
    const { getByTestId, queryByTestId } = _render('album', _enrichment());
    expect(getByTestId('detail-deezer-label')).toHaveTextContent('Daft Life Ltd./ADA France');
    expect(getByTestId('detail-deezer-genre-0')).toHaveTextContent('Electro');
    expect(getByTestId('detail-deezer-genre-1')).toHaveTextContent('Dance');
    // BPM/explicit are track-only — not shown on an album.
    expect(queryByTestId('detail-deezer-meta')).toBeNull();
  });

  it('renders nothing when enrichment is null', () => {
    const { queryByTestId } = _render('track', null);
    expect(queryByTestId('detail-deezer')).toBeNull();
  });

  it('renders nothing for a track with only non-displayed fields (gain/upc)', () => {
    const { queryByTestId } = _render('track', {
      bpm: 0,
      gain: -8.3,
      explicit: false,
      label: '',
      genres: [],
      upc: '724384960650',
      record_type: '',
    });
    expect(queryByTestId('detail-deezer')).toBeNull();
  });
});
