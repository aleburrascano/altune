import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { FilterChips } from '../ui/FilterChips';

describe('the discover filter row offers all and one chip per kind', () => {
  it('labels the four chips in a fixed order', () => {
    render(<FilterChips active="all" onSelect={jest.fn()} />);

    expect(screen.getByTestId('discover-filter-all')).toHaveTextContent('All');
    expect(screen.getByTestId('discover-filter-album')).toHaveTextContent('Albums');
    expect(screen.getByTestId('discover-filter-track')).toHaveTextContent('Tracks');
    expect(screen.getByTestId('discover-filter-artist')).toHaveTextContent('Artists');
  });

  it('marks only the active chip selected', () => {
    render(<FilterChips active="album" onSelect={jest.fn()} />);

    expect(screen.getByTestId('discover-filter-album').props.accessibilityState.selected).toBe(true);
    expect(screen.getByTestId('discover-filter-all').props.accessibilityState.selected).toBe(false);
  });

  it('reports the tapped chip’s filter to its caller', () => {
    const onSelect = jest.fn();
    render(<FilterChips active="all" onSelect={onSelect} />);

    fireEvent.press(screen.getByTestId('discover-filter-track'));

    expect(onSelect).toHaveBeenCalledWith('track');
  });
});
