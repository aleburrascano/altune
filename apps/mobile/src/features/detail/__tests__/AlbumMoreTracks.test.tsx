// #2816: AlbumMoreTracks now draws its header with the shared
// CollapsibleSectionHeader; its props are unchanged, so these pin what a caller
// of the section sees: the interactive header, the static failure header and retry.

import React from 'react';
import { StyleSheet } from 'react-native';
import { render, screen, fireEvent } from '@testing-library/react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { AlbumMoreTracks } from '../ui/AlbumMoreTracks';

function track(title: string): DiscoveryResult {
  return {
    kind: 'track',
    title,
    subtitle: 'Fleetwood Mac',
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'deezer', external_id: `d-${title}`, url: `https://d/${title}` }],
    extras: {},
  };
}

type RowActionOverrides = {
  ownedFor?: React.ComponentProps<typeof AlbumMoreTracks>['rowActions']['ownedFor'];
  isSavingInBatch?: React.ComponentProps<typeof AlbumMoreTracks>['rowActions']['isSavingInBatch'];
  onTrackPress?: React.ComponentProps<typeof AlbumMoreTracks>['rowActions']['onTrackPress'];
  onQuickSave?: React.ComponentProps<typeof AlbumMoreTracks>['rowActions']['onQuickSave'];
};

function renderMore(
  overrides: Partial<Omit<React.ComponentProps<typeof AlbumMoreTracks>, 'rowActions'>> &
    RowActionOverrides = {},
) {
  const { ownedFor, isSavingInBatch, onTrackPress, onQuickSave, ...rest } = overrides;
  const props: React.ComponentProps<typeof AlbumMoreTracks> = {
    tracks: [track('Dreams'), track('Songbird')],
    baseIndex: 1,
    expanded: false,
    onToggle: jest.fn(),
    savingAll: false,
    onSaveAll: jest.fn(),
    rowActions: {
      ownedFor: ownedFor ?? (() => null),
      isSavingInBatch: isSavingInBatch ?? (() => false),
      onTrackPress: onTrackPress ?? jest.fn(),
      onQuickSave: onQuickSave ?? jest.fn(),
    },
    failure: null,
    onRetry: jest.fn(),
    ...rest,
  };
  render(<AlbumMoreTracks {...props} />);
  return props;
}

describe('AlbumMoreTracks: the interactive header', () => {
  it('toggles through its header and keeps the tracks hidden while collapsed', () => {
    const props = renderMore();

    expect(screen.queryByText('Dreams')).toBeNull();
    fireEvent.press(screen.getByTestId('detail-more-from-album'));

    expect(props.onToggle).toHaveBeenCalledTimes(1);
  });

  it('lists every extra track once expanded', () => {
    renderMore({ expanded: true });

    expect(screen.getByText('Dreams')).toBeTruthy();
    expect(screen.getByText('Songbird')).toBeTruthy();
  });

  it('presents the header as a button at least 48pt tall', () => {
    renderMore();

    const header = screen.getByTestId('detail-more-from-album');
    expect(header.props.accessibilityRole).toBe('button');
    expect(StyleSheet.flatten(header.props.style)?.minHeight).toBeGreaterThanOrEqual(48);
  });

  it('renders nothing when there are no extra tracks and no failure', () => {
    renderMore({ tracks: [] });

    expect(screen.queryByTestId('detail-more-from-album')).toBeNull();
    expect(screen.queryByTestId('detail-more-from-album-error')).toBeNull();
  });
});

describe('AlbumMoreTracks: the static failure header', () => {
  it('shows the error with a retry that calls onRetry on a transient failure', () => {
    const props = renderMore({ tracks: [], failure: 'transient' });

    expect(screen.getByTestId('detail-more-from-album-error')).toBeTruthy();
    fireEvent.press(screen.getByTestId('detail-more-from-album-retry'));

    expect(props.onRetry).toHaveBeenCalledTimes(1);
    expect(props.onToggle).not.toHaveBeenCalled();
  });

  it('offers no toggle button in its header while failed', () => {
    renderMore({ tracks: [], failure: 'transient' });

    expect(screen.queryByRole('button', { name: /more/i })).toBeNull();
    expect(screen.queryByTestId('detail-more-from-album')).toBeNull();
  });

  it('drops the retry on a settled failure', () => {
    renderMore({ tracks: [], failure: 'settled' });

    expect(screen.getByTestId('detail-more-from-album-settled')).toBeTruthy();
    expect(screen.queryByTestId('detail-more-from-album-retry')).toBeNull();
  });
});
