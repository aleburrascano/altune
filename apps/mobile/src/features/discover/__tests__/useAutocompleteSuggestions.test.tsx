import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { suggestDiscovery } from '@shared/api-client/discovery';
import {
  SUGGEST_DEBOUNCE_MS,
  useAutocompleteSuggestions,
} from '../hooks/useAutocompleteSuggestions';

jest.mock('@shared/api-client/discovery', () => ({ suggestDiscovery: jest.fn() }));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const mockSuggest = suggestDiscovery as jest.Mock;

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function renderSuggestions() {
  return renderHook(({ text }: { text: string }) => useAutocompleteSuggestions(text), {
    wrapper,
    initialProps: { text: '' },
  });
}

type Rendered = ReturnType<typeof renderSuggestions>;

// Each keystroke lands well inside the debounce window, so the burst is the
// ticket's scenario: a pasted or fast-typed query, not a pause between letters.
function typeBurst(rendered: Rendered, text: string): void {
  for (let length = 1; length <= text.length; length += 1) {
    act(() => {
      rendered.rerender({ text: text.slice(0, length) });
      jest.advanceTimersByTime(SUGGEST_DEBOUNCE_MS - 1);
    });
  }
}

function settle(): void {
  act(() => {
    jest.advanceTimersByTime(SUGGEST_DEBOUNCE_MS);
  });
}

function signalOfCall(index: number): AbortSignal | undefined {
  return mockSuggest.mock.calls[index]?.[1] as AbortSignal | undefined;
}

beforeEach(() => {
  jest.useFakeTimers();
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  mockSuggest.mockReset().mockResolvedValue({ suggestions: [] });
});

afterEach(() => {
  queryClient.clear();
  jest.runOnlyPendingTimers();
  jest.useRealTimers();
});

describe('useAutocompleteSuggestions asks for suggestions once the typing settles', () => {
  it('sends one request for a burst of keystrokes typed inside the debounce window', () => {
    const rendered = renderSuggestions();

    typeBurst(rendered, 'radiohead');
    settle();

    expect(mockSuggest).toHaveBeenCalledTimes(1);
    expect(mockSuggest).toHaveBeenCalledWith({ q: 'radiohead', limit: 5 }, expect.anything());
  });

  it('holds the request back until the typing has paused for the debounce window', () => {
    const rendered = renderSuggestions();

    typeBurst(rendered, 'rad');

    expect(mockSuggest).not.toHaveBeenCalled();
  });
});

describe('useAutocompleteSuggestions cancels a suggest request its successor supersedes', () => {
  it('aborts the in-flight request when a later query starts', () => {
    mockSuggest.mockImplementation(() => new Promise(() => {}));
    const rendered = renderSuggestions();

    typeBurst(rendered, 'rad');
    settle();
    typeBurst(rendered, 'radiohead');
    settle();

    expect(mockSuggest).toHaveBeenCalledTimes(2);
    expect(signalOfCall(0)?.aborted).toBe(true);
    expect(signalOfCall(1)?.aborted).toBe(false);
  });
});
