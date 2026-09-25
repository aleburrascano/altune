import { act, renderHook } from '@testing-library/react-native';

import { useResultsFilter } from '../hooks/useResultsFilter';

describe('useResultsFilter resets to "all" only when a new query is committed', () => {
  it('keeps the chosen filter while the query is unchanged and resets it on a new query', () => {
    const { result, rerender } = renderHook(
      ({ query }: { query: string }) => useResultsFilter(query),
      {
        initialProps: { query: 'radiohead' },
      },
    );

    act(() => result.current.setFilter('track'));
    rerender({ query: 'radiohead' });
    expect(result.current.filter).toBe('track');

    rerender({ query: 'bjork' });
    expect(result.current.filter).toBe('all');
  });
});
