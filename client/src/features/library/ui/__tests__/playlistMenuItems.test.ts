import { menuItems, type MenuEditProps } from '@features/library/ui/playlistMenuItems';

const DOWNLOAD_LABEL_PATTERN = /download/i;

const props: MenuEditProps = {
  onAddTracks: () => undefined,
  rename: { startEditing: () => undefined },
};

const downloadAll = { label: 'Download all (1)', onPress: () => undefined };

function labels(offlineSupported: boolean): string[] {
  return menuItems(props, () => undefined, downloadAll, offlineSupported).map((i) => i.label);
}

function isDownload(label: string): boolean {
  return DOWNLOAD_LABEL_PATTERN.test(label);
}

describe('playlist options menu items', () => {
  it('offers no download action where offline downloads are unsupported (web)', () => {
    const result = labels(false);
    expect(result).toContain('Delete Playlist');
    expect(result.some(isDownload)).toBe(false);
  });

  it('offers the download action where offline downloads are supported (native)', () => {
    expect(labels(true)).toEqual([
      'Add Tracks',
      'Rename Playlist',
      'Download all (1)',
      'Delete Playlist',
    ]);
  });
});
