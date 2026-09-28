import { render, screen } from '@testing-library/react-native';

import { Artwork } from '../Artwork';

const COVER = 'https://cdn.altune.test/cover.jpg';

function sourceUris(source: unknown): (string | undefined)[] {
  const entries = Array.isArray(source) ? source : [source];
  return entries.map((entry) =>
    entry != null && typeof entry === 'object' && 'uri' in entry
      ? (entry as { uri?: string }).uri
      : undefined,
  );
}

function artworkSource() {
  return screen.getByTestId('artwork').props.source;
}

describe('Artwork(): a null uri never retains the previously shown cover', () => {
  it('shows the covered uri as its source when one is present', () => {
    render(<Artwork uri={COVER} />);

    expect(sourceUris(artworkSource())).toContain(COVER);
  });

  it('drops the prior cover and falls back to a placeholder when uri becomes null', () => {
    const { rerender } = render(<Artwork uri={COVER} />);
    expect(sourceUris(artworkSource())).toContain(COVER);

    rerender(<Artwork uri={null} />);

    expect(sourceUris(artworkSource())).not.toContain(COVER);
  });

  it('remounts on uri change so a recycled instance cannot keep the old bitmap', () => {
    const { rerender } = render(<Artwork uri={COVER} />);
    const coveredKey = screen.getByTestId('artwork').props.source;

    rerender(<Artwork uri={null} />);

    expect(artworkSource()).not.toEqual(coveredKey);
  });
});
