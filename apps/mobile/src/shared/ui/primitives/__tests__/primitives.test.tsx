import { fireEvent, render } from '@testing-library/react-native';

import { darkTheme } from '../../theme/darkTheme';
import { Banner } from '../Banner';
import { Chip } from '../Chip';
import { ConfidenceDot } from '../ConfidenceDot';
import { IconButton } from '../IconButton';
import { Text } from '../Text';

// Primitives render with NO ThemeProvider here on purpose — proving the
// useTheme() dark fallback that keeps the bare-rendered auth screens green.

describe('Text', () => {
  it('forwards testID and renders its children (the auth-error contract)', () => {
    const { getByTestId } = render(<Text testID="t">Hello</Text>);
    expect(getByTestId('t')).toBeTruthy();
    expect(getByTestId('t').props.children).toBe('Hello');
  });
});

describe('Banner', () => {
  it('forwards testID and renders truthy children', () => {
    const { getByTestId } = render(<Banner testID="b">Partial results</Banner>);
    const node = getByTestId('b');
    expect(node).toBeTruthy();
    expect(node.props.children).toBeTruthy();
  });
});

describe('ConfidenceDot', () => {
  it('colors the dot by level from the active theme', () => {
    const { getByLabelText } = render(<ConfidenceDot level="high" />);
    expect(getByLabelText('High confidence').props.style).toEqual(
      expect.objectContaining({ backgroundColor: darkTheme.color.confHigh }),
    );
  });

  it('uses the low-confidence color for low', () => {
    const { getByLabelText } = render(<ConfidenceDot level="low" />);
    expect(getByLabelText('Low confidence').props.style).toEqual(
      expect.objectContaining({ backgroundColor: darkTheme.color.confLow }),
    );
  });
});

describe('Chip', () => {
  it('fires onPress when tapped', () => {
    const onPress = jest.fn();
    const { getByTestId } = render(<Chip label="rock" testID="chip" onPress={onPress} />);
    fireEvent.press(getByTestId('chip'));
    expect(onPress).toHaveBeenCalledTimes(1);
  });
});

describe('IconButton', () => {
  it('fires onPress and exposes its accessibility label', () => {
    const onPress = jest.fn();
    const FakeIcon = () => null;
    const { getByLabelText } = render(
      <IconButton icon={FakeIcon} onPress={onPress} accessibilityLabel="Play" />,
    );
    fireEvent.press(getByLabelText('Play'));
    expect(onPress).toHaveBeenCalledTimes(1);
  });
});
