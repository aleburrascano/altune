/**
 * LibraryNoResults — a filtered-out library must never look like a missing one.
 * The view names the active query, says the library is intact, and offers a
 * one-tap clear.
 */
import { render, screen, userEvent } from '@testing-library/react-native';

import { LibraryNoResults } from '../ui/LibraryNoResults';

jest.useFakeTimers();

describe('LibraryNoResults', () => {
  it('names the query that is filtering', () => {
    render(<LibraryNoResults query="reo speedwagon" onClear={jest.fn()} />);
    expect(screen.getByText(/reo speedwagon/)).toBeTruthy();
    expect(screen.getByText(/still here/i)).toBeTruthy();
  });

  it('clears the search on tap', async () => {
    const onClear = jest.fn();
    render(<LibraryNoResults query="reo" onClear={onClear} />);

    await userEvent.setup({ advanceTimers: jest.advanceTimersByTime }).press(
      screen.getByTestId('library-clear-search'),
    );

    expect(onClear).toHaveBeenCalledTimes(1);
  });
});
