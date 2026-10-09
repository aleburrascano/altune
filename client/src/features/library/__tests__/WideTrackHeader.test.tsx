import { render, screen } from '@testing-library/react-native';

import { WideTrackHeader } from '../ui/WideTrackHeader';

describe('WideTrackHeader', () => {
  it('labels every column of the wide tracks table', () => {
    render(<WideTrackHeader />);

    expect(screen.getByTestId('library-wide-track-header')).toBeTruthy();
    expect(screen.getByText('Title')).toBeTruthy();
    expect(screen.getByText('Artist')).toBeTruthy();
    expect(screen.getByText('Album')).toBeTruthy();
    expect(screen.getByText('Duration')).toBeTruthy();
  });
});
