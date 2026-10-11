import { Text } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import * as userTelemetry from '@shared/telemetry/userTelemetry';
import { Tappable } from '@shared/ui/primitives/Tappable';

let record: jest.SpyInstance;

beforeEach(() => {
  record = jest.spyOn(userTelemetry, 'recordUserAction').mockImplementation(() => undefined);
});

describe('Tappable', () => {
  it('records one tapped action with the track id, then calls onPress', () => {
    const order: string[] = [];
    record.mockImplementation(() => order.push('record'));
    const onPress = jest.fn(() => order.push('press'));
    render(
      <Tappable action="a.b" trackId="t1" onPress={onPress} testID="t">
        <Text>x</Text>
      </Tappable>,
    );
    fireEvent.press(screen.getByTestId('t'));
    expect(record).toHaveBeenCalledTimes(1);
    expect(record).toHaveBeenCalledWith({ action: 'a.b', outcome: 'tapped', track_id: 't1' });
    expect(order).toEqual(['record', 'press']);
  });

  it('records nothing and does not press when disabled', () => {
    const onPress = jest.fn();
    render(
      <Tappable action="a.b" onPress={onPress} disabled testID="t">
        <Text>x</Text>
      </Tappable>,
    );
    fireEvent.press(screen.getByTestId('t'));
    expect(record).not.toHaveBeenCalled();
    expect(onPress).not.toHaveBeenCalled();
  });
});
