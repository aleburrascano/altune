import { fireEvent, render, screen } from '@testing-library/react-native';

import { WIDE_LAYOUT_MIN_WIDTH } from '@shared/ui/layout/breakpoints';

import { usePlaybackVolumeStore } from '@features/playback/web/playbackVolume';
import { VolumeSlider } from '@features/playback/web/VolumeSlider';

const CHOSEN = '0.5';

let mockWindowWidth = 390;
let mockIsWebPlatform = true;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

jest.mock('@shared/device/isWebPlatform', () => ({
  isWebPlatform: () => mockIsWebPlatform,
}));

afterEach(() => {
  mockWindowWidth = 390;
  mockIsWebPlatform = true;
  usePlaybackVolumeStore.setState({ volume: 1 });
});

describe('VolumeSlider', () => {
  it('shows the slider on a wide web window', () => {
    mockWindowWidth = WIDE_LAYOUT_MIN_WIDTH;

    render(<VolumeSlider />);

    expect(screen.getByLabelText('Volume')).toBeTruthy();
  });

  it('hides the slider on a narrow web window, where the phone buttons set volume', () => {
    mockWindowWidth = WIDE_LAYOUT_MIN_WIDTH - 1;

    render(<VolumeSlider />);

    expect(screen.queryByLabelText('Volume')).toBeNull();
  });

  it('hides the slider on a wide native window', () => {
    mockWindowWidth = WIDE_LAYOUT_MIN_WIDTH;
    mockIsWebPlatform = false;

    render(<VolumeSlider />);

    expect(screen.queryByLabelText('Volume')).toBeNull();
  });

  it('writes the chosen value to the volume store', () => {
    mockWindowWidth = WIDE_LAYOUT_MIN_WIDTH;
    render(<VolumeSlider />);

    fireEvent(screen.getByLabelText('Volume'), 'change', { currentTarget: { value: CHOSEN } });

    expect(usePlaybackVolumeStore.getState().volume).toBe(Number(CHOSEN));
  });
});
