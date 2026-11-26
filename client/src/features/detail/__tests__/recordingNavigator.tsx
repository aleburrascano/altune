import type { ComponentType, ReactElement, ReactNode } from 'react';
import type { Navigator } from '@shared/navigation';
import { DetailNavigatorProvider } from '@features/detail';
import { createPlaybackWrapper } from '@testing/queryHarness';

interface WrapperProps {
  children: ReactNode;
}

type Wrapper = ComponentType<WrapperProps>;

export interface RecordingNavigator extends Navigator {
  push: jest.Mock;
  replace: jest.Mock;
  back: jest.Mock;
}

export function recordingNavigator(): RecordingNavigator {
  return { push: jest.fn(), replace: jest.fn(), back: jest.fn(), canGoBack: () => false };
}

export function navigatorWrapper(navigator: Navigator, Outer?: Wrapper): Wrapper {
  return function NavigatorWrapper({ children }: WrapperProps): ReactElement {
    const inner = (
      <DetailNavigatorProvider navigator={navigator}>{children}</DetailNavigatorProvider>
    );
    return Outer ? <Outer>{inner}</Outer> : inner;
  };
}

type PlaybackWrapperArgs = Parameters<typeof createPlaybackWrapper>;

export function playbackWithNavigator(...args: PlaybackWrapperArgs): Wrapper {
  return navigatorWrapper(recordingNavigator(), createPlaybackWrapper(...args));
}
