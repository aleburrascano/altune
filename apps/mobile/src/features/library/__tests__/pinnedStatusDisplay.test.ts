import { ArrowDownCircle, CircleAlert, CircleCheck } from 'lucide-react-native';

import { pinnedStatusDisplay } from '../pinnedStatusDisplay';

describe('pinnedStatusDisplay — one bucket decides the spoken label, the icon and the menu copy', () => {
  it('pairs a finished download with the check icon and an offer to remove it', () => {
    const { a11ySuffix, icon, menu } = pinnedStatusDisplay('ready');
    expect(a11ySuffix).toBe(', downloaded');
    expect(icon).toEqual({
      glyph: CircleCheck,
      color: 'accent',
      testIdPrefix: 'library-row-offline',
    });
    expect(menu).toEqual({ label: 'Remove download', action: 'unpin' });
  });

  it('treats queued and downloading as one in-flight download the user can cancel', () => {
    expect(pinnedStatusDisplay('queued')).toEqual(pinnedStatusDisplay('downloading'));
    const { a11ySuffix, icon, menu } = pinnedStatusDisplay('queued');
    expect(a11ySuffix).toBe(', downloading');
    expect(icon).toEqual({
      glyph: ArrowDownCircle,
      color: 'textTertiary',
      testIdPrefix: 'library-row-offline-pending',
    });
    expect(menu).toEqual({ label: 'Cancel download', action: 'unpin' });
  });

  it('pairs a failed download with the alert icon and an offer to retry it', () => {
    const { a11ySuffix, icon, menu } = pinnedStatusDisplay('failed');
    expect(a11ySuffix).toBe(', download failed');
    expect(icon).toEqual({
      glyph: CircleAlert,
      color: 'danger',
      testIdPrefix: 'library-row-offline-failed',
    });
    expect(menu).toEqual({ label: 'Retry download', action: 'pin' });
  });

  it('says and shows nothing for a track that was never pinned, but offers the download', () => {
    const { a11ySuffix, icon, menu } = pinnedStatusDisplay(undefined);
    expect(a11ySuffix).toBe('');
    expect(icon).toBeNull();
    expect(menu).toEqual({ label: 'Download', action: 'pin' });
  });
});
