// #2816: one collapsible chevron header shared by the artist explore section and
// AlbumMoreTracks. Contract from the ticket:
// CollapsibleSectionHeader({ label, expanded, onToggle?, testID, accessibilityLabel }).

import React from 'react';
import { StyleSheet } from 'react-native';
import { render, screen, fireEvent } from '@testing-library/react-native';

import { CollapsibleSectionHeader } from '../ui/CollapsibleSectionHeader';

describe('CollapsibleSectionHeader with a toggle', () => {
  it('shows its label and calls onToggle once per press', () => {
    const onToggle = jest.fn();
    render(
      <CollapsibleSectionHeader
        label="More from this album"
        expanded={false}
        onToggle={onToggle}
        testID="probe-header"
        accessibilityLabel="Show more tracks"
      />,
    );

    expect(screen.getByText('More from this album')).toBeTruthy();
    fireEvent.press(screen.getByTestId('probe-header'));
    fireEvent.press(screen.getByTestId('probe-header'));

    expect(onToggle).toHaveBeenCalledTimes(2);
  });

  it('carries the caller accessibility label on a button', () => {
    render(
      <CollapsibleSectionHeader
        label="Explore"
        expanded
        onToggle={() => {}}
        testID="probe-header"
        accessibilityLabel="Collapse discography"
      />,
    );

    const header = screen.getByTestId('probe-header');
    expect(header.props.accessibilityLabel).toBe('Collapse discography');
    expect(header.props.accessibilityRole).toBe('button');
    expect(screen.getByRole('button', { name: 'Collapse discography' })).toBeTruthy();
  });

  it('keeps an interactive header at least 48pt tall', () => {
    render(
      <CollapsibleSectionHeader
        label="Explore"
        expanded={false}
        onToggle={() => {}}
        testID="probe-header"
        accessibilityLabel="Explore full discography"
      />,
    );

    const style = StyleSheet.flatten(screen.getByTestId('probe-header').props.style) ?? {};
    expect(style.minHeight).toBeGreaterThanOrEqual(48);
  });
});

describe('CollapsibleSectionHeader without a toggle', () => {
  it('renders its label as a static header that offers no button', () => {
    render(
      <CollapsibleSectionHeader label="More from this album" expanded />,
    );

    expect(screen.getByText('More from this album')).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
  });
});
