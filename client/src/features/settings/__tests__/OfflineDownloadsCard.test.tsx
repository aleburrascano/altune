import React from 'react';
import { render, screen, within } from '@testing-library/react-native';

import { darkTheme } from '@shared/ui';

import type { DownloadStats } from '@features/settings/domain/downloadStatsModel';
import { OfflineDownloadsCard } from '@features/settings/ui/OfflineDownloadsCard';

function makeStats(over: Partial<DownloadStats> = {}): DownloadStats {
  return {
    downloadCount: 0,
    downloadBytes: 0,
    downloadSize: '0 B',
    usage: 'none',
    usageLabel: 'No downloads on this device',
    usageDetail: undefined,
    ...over,
  };
}

const row = () => within(screen.getByTestId('settings-downloads-usage'));

type RenderedNode = ReturnType<typeof screen.toJSON> | string;

function renderedStrokes(): string[] {
  const strokes = new Set<string>();
  function visit(node: RenderedNode): void {
    if (node == null || typeof node === 'string') return;
    if (Array.isArray(node)) {
      node.forEach(visit);
      return;
    }
    if (typeof node.props.stroke === 'string') strokes.add(node.props.stroke);
    node.children?.forEach(visit);
  }
  visit(screen.toJSON());
  return [...strokes];
}

describe('OfflineDownloadsCard', () => {
  it('renders a neutral row with no detail when nothing is downloaded', () => {
    render(<OfflineDownloadsCard stats={makeStats()} />);
    expect(row().getByText('No downloads on this device')).toBeTruthy();
    expect(row().queryByText(/MB|KB|B$/)).toBeNull();
    expect(renderedStrokes()).toEqual([darkTheme.color.textSecondary]);
  });

  it('renders a neutral row with the leftover-bytes copy when files remain but no track is ready', () => {
    render(
      <OfflineDownloadsCard
        stats={makeStats({
          downloadBytes: 4 * 1024 ** 2,
          downloadSize: '4 MB',
          usage: 'leftover',
          usageLabel: 'Leftover download files',
          usageDetail: '4 MB',
        })}
      />,
    );
    expect(row().getByText('Leftover download files')).toBeTruthy();
    expect(row().getByText('4 MB')).toBeTruthy();
    expect(renderedStrokes()).toEqual([darkTheme.color.textSecondary]);
  });

  it('renders a success-toned row with the track count and size when downloads exist', () => {
    render(
      <OfflineDownloadsCard
        stats={makeStats({
          downloadCount: 3,
          downloadBytes: 12 * 1024 ** 2,
          downloadSize: '12 MB',
          usage: 'tracks',
          usageLabel: '3 tracks',
          usageDetail: '12 MB',
        })}
      />,
    );
    expect(row().getByText('3 tracks')).toBeTruthy();
    expect(row().getByText('12 MB')).toBeTruthy();
    expect(renderedStrokes()).toEqual([darkTheme.color.success]);
  });
});
