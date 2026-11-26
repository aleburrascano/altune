import { QueryClientProvider, useQuery } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react-native';
import { Text } from 'react-native';

import { createTestQueryClient } from '@testing/queryHarness';

function Probe({ answer }: { answer: Promise<string> }) {
  const query = useQuery({ queryKey: ['probe'], queryFn: () => answer });
  return <Text>{query.data ?? query.status}</Text>;
}

function nextTick(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

describe('createTestQueryClient', () => {
  it('delivers a query result that settles after the render without an act warning', async () => {
    const errors: unknown[][] = [];
    const original = console.error;
    console.error = (...args: unknown[]) => {
      errors.push(args);
    };
    let settle: (value: string) => void = () => undefined;
    const answer = new Promise<string>((resolve) => {
      settle = resolve;
    });
    render(
      <QueryClientProvider client={createTestQueryClient()}>
        <Probe answer={answer} />
      </QueryClientProvider>,
    );

    settle('answered');
    await nextTick();
    await nextTick();

    console.error = original;
    expect(screen.getByText('answered')).toBeTruthy();
    expect(errors.filter((args) => String(args[0]).includes('not wrapped in act'))).toEqual([]);
  });
});
