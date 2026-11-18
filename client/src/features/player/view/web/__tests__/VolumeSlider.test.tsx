import { render, screen } from '@testing-library/react-native';

import { WIDE_LAYOUT_MIN_WIDTH } from '@shared/ui';

import { setWindowWidth } from '@testing/setWindowWidth';

import { VolumeSlider } from '@features/player';

describe('VolumeSlider on a wide native window', () => {
  it('renders nothing, because native volume belongs to the device', () => {
    setWindowWidth(WIDE_LAYOUT_MIN_WIDTH);

    render(<VolumeSlider />);

    expect(screen.queryByLabelText('Volume')).toBeNull();
  });
});
