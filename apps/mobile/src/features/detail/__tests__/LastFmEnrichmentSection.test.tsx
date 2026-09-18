import React from 'react';
import { fireEvent, render } from '@testing-library/react-native';

import type { LastFmEnrichmentResponse } from '@shared/api-client/enrichment';

import { LastFmEnrichmentSection } from '../ui/LastFmEnrichmentSection';

// A failed Last.fm fetch and an artist with no Last.fm page both arrive here as
// `enrichment === null`. These tests hold the seam that keeps them apart, so a
// later notice/retry treatment has something to branch on.

function emptyEnrichment(): LastFmEnrichmentResponse {
  return {
    has_content: true,
    mbid: '',
    listeners: 0,
    playcount: 0,
    tags: [],
    bio: '',
    similar: [],
    duration: 0,
    album: '',
  };
}

// Past the 220-character threshold that earns the Read more/Read less toggle.
function longBioEnrichment(): LastFmEnrichmentResponse {
  return { ...emptyEnrichment(), bio: 'Radiohead formed in Abingdon. '.repeat(10) };
}

describe('LastFmEnrichmentSection: a failed fetch vs an artist with nothing to show', () => {
  it('marks the section unavailable when the fetch failed', () => {
    const { queryByTestId } = render(<LastFmEnrichmentSection enrichment={null} isError />);

    expect(queryByTestId('detail-lastfm-unavailable')).not.toBeNull();
  });

  it('renders nothing at all when the artist genuinely has no Last.fm content', () => {
    const { queryByTestId } = render(
      <LastFmEnrichmentSection enrichment={null} isError={false} />,
    );

    expect(queryByTestId('detail-lastfm-unavailable')).toBeNull();
    expect(queryByTestId('detail-lastfm')).toBeNull();
  });

  it('renders nothing at all when the fetch succeeded with an empty bio and no similar artists', () => {
    const { queryByTestId } = render(
      <LastFmEnrichmentSection enrichment={emptyEnrichment()} isError={false} />,
    );

    expect(queryByTestId('detail-lastfm-unavailable')).toBeNull();
    expect(queryByTestId('detail-lastfm')).toBeNull();
  });
});

describe('LastFmEnrichmentSection: expanding a long bio', () => {
  it('shows the full bio after Read more is pressed', () => {
    const { getByTestId } = render(
      <LastFmEnrichmentSection enrichment={longBioEnrichment()} isError={false} />,
    );

    fireEvent.press(getByTestId('detail-lastfm-bio-toggle'));

    expect(getByTestId('detail-lastfm-bio').props.numberOfLines).toBeUndefined();
  });

  it('truncates the bio until the toggle is pressed', () => {
    const { getByTestId } = render(
      <LastFmEnrichmentSection enrichment={longBioEnrichment()} isError={false} />,
    );

    expect(getByTestId('detail-lastfm-bio').props.numberOfLines).toBe(4);
  });
});
