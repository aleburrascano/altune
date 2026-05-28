import { fireEvent, render } from '@testing-library/react-native';

import { Button } from '../Button';
import { Skeleton } from '../Skeleton';

// Button + Skeleton use RN's built-in Animated for press feedback + shimmer
// (no react-native-reanimated — see ADR-0008), so they render in jest with no
// extra mocks. The auth screens reuse Button after the retrofit.

describe('Button', () => {
  it('renders its label and fires onPress', () => {
    const onPress = jest.fn();
    const { getByText } = render(<Button label="Sign in" onPress={onPress} testID="btn" />);
    fireEvent.press(getByText('Sign in'));
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('does not fire onPress while loading', () => {
    const onPress = jest.fn();
    const { getByTestId } = render(
      <Button label="Sign in" onPress={onPress} loading testID="btn" />,
    );
    fireEvent.press(getByTestId('btn'));
    expect(onPress).not.toHaveBeenCalled();
  });
});

describe('Skeleton', () => {
  it('renders without crashing (shimmer + reduce-motion safe)', () => {
    const { toJSON } = render(<Skeleton width={100} height={20} />);
    expect(toJSON()).toBeTruthy();
  });
});
