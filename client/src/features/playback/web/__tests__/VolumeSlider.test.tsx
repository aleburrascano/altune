import { render, screen } from '@testing-library/react-native';

import { WIDE_LAYOUT_MIN_WIDTH } from '@shared/ui/layout/breakpoints';

import { VolumeSlider } from '@features/playback/web/VolumeSlider';

let mockWindowWidth = WIDE_LAYOUT_MIN_WIDTH;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 800, scale: 2, fontScale: 1 }),
}));

describe('VolumeSlider on a wide native window', () => {
  it('renders nothing, because native volume belongs to the device', () => {
    render(<VolumeSlider />);

    expect(screen.queryByLabelText('Volume')).toBeNull();
  });
});
