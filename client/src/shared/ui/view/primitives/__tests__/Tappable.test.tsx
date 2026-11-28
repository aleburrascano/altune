import { Text } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { createAppTelemetry, installTelemetry, type ClientEvent } from '@shared/telemetry';
import { Tappable } from '@shared/ui';

let sent: ClientEvent[];
let order: string[];

function recordSent(event: ClientEvent): Promise<void> {
  sent.push(event);
  order.push('record');
  return Promise.resolve();
}

beforeEach(() => {
  sent = [];
  order = [];
  installTelemetry(createAppTelemetry({ send: recordSent }));
});

describe('Tappable', () => {
  it('records one tapped action with the track id, then calls onPress', () => {
    const onPress = jest.fn(() => order.push('press'));
    render(
      <Tappable action="a.b" trackId="t1" onPress={onPress} testID="t">
        <Text>x</Text>
      </Tappable>,
    );
    fireEvent.press(screen.getByTestId('t'));
    expect(sent).toHaveLength(1);
    expect(sent[0]).toMatchObject({
      type: 'user_action',
      payload: { action: 'a.b', outcome: 'tapped', track_id: 't1' },
    });
    expect(order).toEqual(['record', 'press']);
  });

  it('passes the press event to onPress', () => {
    const onPress = jest.fn();
    render(
      <Tappable action="a.b" onPress={onPress} testID="t">
        <Text>x</Text>
      </Tappable>,
    );
    const event = { stopPropagation: jest.fn() };
    fireEvent.press(screen.getByTestId('t'), event);
    expect(onPress).toHaveBeenCalledWith(expect.objectContaining(event));
  });

  it('records nothing and does not press when disabled', () => {
    const onPress = jest.fn();
    render(
      <Tappable action="a.b" onPress={onPress} disabled testID="t">
        <Text>x</Text>
      </Tappable>,
    );
    fireEvent.press(screen.getByTestId('t'));
    expect(sent).toEqual([]);
    expect(onPress).not.toHaveBeenCalled();
  });
});
