import { Text } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { recordUserAction } from '../../../telemetry/userTelemetry';
import { Button } from '../Button';
import { Chip } from '../Chip';
import { IconButton } from '../IconButton';

jest.mock('../../../telemetry/userTelemetry', () => ({ recordUserAction: jest.fn() }));
jest.mock('../../haptics', () => ({ tapFeedback: jest.fn() }));

const record = recordUserAction as jest.Mock;

function Icon() {
  return <Text>icon</Text>;
}

beforeEach(() => record.mockReset());

describe('Button', () => {
  it('records one tapped action with the track id, then calls onPress', () => {
    const order: string[] = [];
    record.mockImplementation(() => order.push('record'));
    const onPress = jest.fn(() => order.push('press'));
    render(<Button label="Go" action="auth.sign_in" trackId="t1" onPress={onPress} testID="b" />);
    fireEvent.press(screen.getByTestId('b'));
    expect(record).toHaveBeenCalledTimes(1);
    expect(record).toHaveBeenCalledWith({
      action: 'auth.sign_in',
      outcome: 'tapped',
      track_id: 't1',
    });
    expect(order).toEqual(['record', 'press']);
  });

  it('only calls onPress when there is no action', () => {
    const onPress = jest.fn();
    render(<Button label="Go" onPress={onPress} testID="b" />);
    fireEvent.press(screen.getByTestId('b'));
    expect(onPress).toHaveBeenCalledTimes(1);
    expect(record).not.toHaveBeenCalled();
  });

  it.each([{ disabled: true }, { loading: true }])('records nothing when %p', (flags) => {
    const onPress = jest.fn();
    render(<Button label="Go" action="a.b" onPress={onPress} testID="b" {...flags} />);
    fireEvent.press(screen.getByTestId('b'));
    expect(record).not.toHaveBeenCalled();
    expect(onPress).not.toHaveBeenCalled();
  });
});

describe('IconButton', () => {
  it('records one tapped action then calls onPress', () => {
    const onPress = jest.fn();
    render(
      <IconButton
        icon={Icon}
        accessibilityLabel="x"
        action="playback.mini.play_pause"
        trackId="t2"
        onPress={onPress}
        testID="i"
      />,
    );
    fireEvent.press(screen.getByTestId('i'));
    expect(record).toHaveBeenCalledTimes(1);
    expect(record).toHaveBeenCalledWith({
      action: 'playback.mini.play_pause',
      outcome: 'tapped',
      track_id: 't2',
    });
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('records nothing when disabled', () => {
    const onPress = jest.fn();
    render(
      <IconButton
        icon={Icon}
        accessibilityLabel="x"
        action="a.b"
        onPress={onPress}
        disabled
        testID="i"
      />,
    );
    fireEvent.press(screen.getByTestId('i'));
    expect(record).not.toHaveBeenCalled();
    expect(onPress).not.toHaveBeenCalled();
  });
});

describe('Chip', () => {
  it('records one tapped action then calls onPress', () => {
    const onPress = jest.fn();
    render(
      <Chip label="Downloaded" action="library.chip.downloaded" onPress={onPress} testID="c" />,
    );
    fireEvent.press(screen.getByTestId('c'));
    expect(record).toHaveBeenCalledTimes(1);
    expect(record).toHaveBeenCalledWith({
      action: 'library.chip.downloaded',
      outcome: 'tapped',
      track_id: undefined,
    });
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('only calls onPress when there is no action', () => {
    const onPress = jest.fn();
    render(<Chip label="x" onPress={onPress} testID="c" />);
    fireEvent.press(screen.getByTestId('c'));
    expect(onPress).toHaveBeenCalledTimes(1);
    expect(record).not.toHaveBeenCalled();
  });
});
