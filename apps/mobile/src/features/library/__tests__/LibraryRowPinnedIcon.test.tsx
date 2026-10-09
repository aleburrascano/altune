import { ArrowDownCircle, CircleAlert, CircleCheck } from 'lucide-react-native';
import { render } from '@testing-library/react-native';

import { asTrackId } from '@shared/api-client/ids';

import { LibraryRowPinnedIcon, PINNED_GLYPH } from '@features/library/ui/LibraryRowPinnedIcon';

const ID = asTrackId('track-1');

function renderedTestId(tree: ReturnType<ReturnType<typeof render>['toJSON']>): unknown {
  if (tree === null || Array.isArray(tree)) return null;
  return tree.props['data-testid'];
}

describe('LibraryRowPinnedIcon', () => {
  it('maps each display kind to its Lucide glyph', () => {
    expect(PINNED_GLYPH).toEqual({
      downloaded: CircleCheck,
      downloading: ArrowDownCircle,
      failed: CircleAlert,
    });
    expect(PINNED_GLYPH.downloaded).toBe(CircleCheck);
    expect(PINNED_GLYPH.downloading).toBe(ArrowDownCircle);
    expect(PINNED_GLYPH.failed).toBe(CircleAlert);
  });

  it('renders nothing for a track that was never pinned', () => {
    const { toJSON } = render(<LibraryRowPinnedIcon trackId={ID} status={undefined} />);
    expect(toJSON()).toBeNull();
  });

  it('renders the failed testID for a failed download', () => {
    const { toJSON } = render(<LibraryRowPinnedIcon trackId={ID} status="failed" />);
    expect(renderedTestId(toJSON())).toBe('library-row-offline-failed-track-1');
  });

  it('swaps the testID prefix when the same row goes from queued to ready', () => {
    const { rerender, toJSON } = render(<LibraryRowPinnedIcon trackId={ID} status="queued" />);
    expect(renderedTestId(toJSON())).toBe('library-row-offline-pending-track-1');

    rerender(<LibraryRowPinnedIcon trackId={ID} status="ready" />);
    expect(renderedTestId(toJSON())).toBe('library-row-offline-track-1');
  });
});
