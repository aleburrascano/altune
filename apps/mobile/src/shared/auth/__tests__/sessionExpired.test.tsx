import { Text } from 'react-native';
import { act, render, screen } from '@testing-library/react-native';

import {
  _listenerCountForTest,
  clearSessionExpired,
  markSessionExpired,
  useSessionExpired,
} from '../sessionExpired';

function SessionGate() {
  const expired = useSessionExpired();
  return <Text>{expired ? 'expired' : 'active'}</Text>;
}

beforeEach(() => {
  act(() => {
    clearSessionExpired();
  });
});

afterEach(() => {
  act(() => {
    clearSessionExpired();
  });
});

describe('useSessionExpired — liveness through a mounted consumer', () => {
  it('re-renders a mounted consumer to "expired" when markSessionExpired is called from outside React', () => {
    render(<SessionGate />);
    expect(screen.getByText('active')).toBeTruthy();

    act(() => {
      markSessionExpired();
    });

    expect(screen.getByText('expired')).toBeTruthy();
    expect(screen.queryByText('active')).toBeNull();
  });

  it('re-renders back to "active" when clearSessionExpired follows a mark', () => {
    render(<SessionGate />);
    act(() => {
      markSessionExpired();
    });
    expect(screen.getByText('expired')).toBeTruthy();

    act(() => {
      clearSessionExpired();
    });

    expect(screen.getByText('active')).toBeTruthy();
    expect(screen.queryByText('expired')).toBeNull();
  });

  it('unsubscribes on unmount: a later mark no longer reaches the unmounted consumer, and a still-mounted sibling keeps working', () => {
    const gone = render(<SessionGate />);
    const stillMounted = render(<SessionGate />);
    const errorSpy = jest.spyOn(console, 'error').mockImplementation(() => {});

    gone.unmount();

    act(() => {
      markSessionExpired();
    });

    expect(stillMounted.getByText('expired')).toBeTruthy();
    expect(errorSpy).not.toHaveBeenCalled();

    errorSpy.mockRestore();
  });
});

describe('_listenerCountForTest — the listeners set tracks mount/unmount exactly', () => {
  it('starts empty, grows by exactly one per mounted consumer, and shrinks back to the prior size on unmount', () => {
    expect(_listenerCountForTest()).toBe(0);

    const a = render(<SessionGate />);
    expect(_listenerCountForTest()).toBe(1);

    const b = render(<SessionGate />);
    expect(_listenerCountForTest()).toBe(2);

    a.unmount();
    expect(_listenerCountForTest()).toBe(1);

    b.unmount();
    expect(_listenerCountForTest()).toBe(0);
  });

  it('returns to the same count across a mount -> unmount -> remount cycle', () => {
    expect(_listenerCountForTest()).toBe(0);

    const first = render(<SessionGate />);
    expect(_listenerCountForTest()).toBe(1);

    first.unmount();
    expect(_listenerCountForTest()).toBe(0);

    const second = render(<SessionGate />);
    expect(_listenerCountForTest()).toBe(1);

    second.unmount();
    expect(_listenerCountForTest()).toBe(0);
  });
});
