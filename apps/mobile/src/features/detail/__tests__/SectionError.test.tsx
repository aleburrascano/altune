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

  // #1663: a settled failure is one the server has already decided, so the
  // retry that used to be offered here could only fail again.
  it('replaces the retry with a try-later note when the failure is settled', () => {
    render(
      <SectionError
        testIDPrefix="x"
        message="Couldn't load tracks."
        onRetry={jest.fn()}
        failure="settled"
      />,
    );

    expect(screen.queryByTestId('x-retry')).toBeNull();
    expect(screen.getByTestId('x-settled')).toBeTruthy();
  });

  it('keeps the retry for a transient failure', () => {
    render(
      <SectionError
        testIDPrefix="x"
        message="Couldn't load tracks."
        onRetry={jest.fn()}
        failure="transient"
      />,
    );

    expect(screen.getByTestId('x-retry')).toBeTruthy();
    expect(screen.queryByTestId('x-settled')).toBeNull();
  });
});
