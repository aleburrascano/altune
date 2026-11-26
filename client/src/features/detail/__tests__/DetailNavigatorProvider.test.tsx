import { renderHook } from '@testing-library/react-native';

import { useOpenFeaturing } from '@features/detail';
import {
  navigatorWrapper,
  recordingNavigator,
} from '@features/detail/__tests__/recordingNavigator';

const ARTIST = { name: 'Guest Star', mbid: null, deezer_id: null };

describe('the navigator detail views open pages with', () => {
  it('is required: hooks that open pages throw when no provider is mounted', () => {
    expect(() => renderHook(() => useOpenFeaturing('/discover/detail'))).toThrow(
      'Detail views must be rendered inside a DetailNavigatorProvider',
    );
  });

  it('is the one the provider was given', () => {
    const navigator = recordingNavigator();

    const { result } = renderHook(() => useOpenFeaturing('/discover/detail'), {
      wrapper: navigatorWrapper(navigator),
    });
    result.current(ARTIST);

    expect(navigator.push).toHaveBeenCalledTimes(1);
  });
});
