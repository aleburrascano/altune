import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { Text } from '@shared/ui/primitives/Text';

import { SectionError } from '../ui/SectionError';

describe('SectionError', () => {
  it('derives the error and retry testIDs from one prefix', () => {
    render(
      <SectionError testIDPrefix="detail-explore" message="Couldn't load." onRetry={jest.fn()} />,
    );

    expect(screen.getByTestId('detail-explore-error')).toBeTruthy();
    expect(screen.getByTestId('detail-explore-retry')).toBeTruthy();
  });

  it('renders the message in the danger tone', () => {
    render(<SectionError testIDPrefix="x" message="Couldn't load albums." onRetry={jest.fn()} />);

    const message = screen.UNSAFE_getByProps({ children: "Couldn't load albums." });
    expect(message.type).toBe(Text);
    expect(message.props.tone).toBe('danger');
  });

  it('calls onRetry when Retry is pressed', () => {
    const onRetry = jest.fn();
    render(<SectionError testIDPrefix="x" message="Couldn't load." onRetry={onRetry} />);

    fireEvent.press(screen.getByTestId('x-retry'));

    expect(onRetry).toHaveBeenCalledTimes(1);
  });
});
