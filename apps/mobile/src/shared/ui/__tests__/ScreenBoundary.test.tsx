import { render } from '@testing-library/react-native';
import { Text as RNText } from 'react-native';

import { ScreenBoundary } from '../ScreenBoundary';

function Boom(): never {
  throw new Error('boom');
}

describe('ScreenBoundary', () => {
  it('renders children when no error is thrown', () => {
    const { getByText, queryByTestId } = render(
      <ScreenBoundary>
        <RNText>healthy screen</RNText>
      </ScreenBoundary>,
    );
    expect(getByText('healthy screen')).toBeTruthy();
    expect(queryByTestId('screen-error')).toBeNull();
  });

  it('shows the fallback and logs when a child throws', () => {
    const errorSpy = jest.spyOn(console, 'error').mockImplementation(() => {});
    const { getByTestId } = render(
      <ScreenBoundary>
        <Boom />
      </ScreenBoundary>,
    );
    expect(getByTestId('screen-error')).toBeTruthy();
    expect(getByTestId('screen-error-retry')).toBeTruthy();
    expect(errorSpy).toHaveBeenCalled();
    errorSpy.mockRestore();
  });
});
