import { Text } from 'react-native';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';

import { signInTestUser } from '@testing/signInTestUser';
import { Button, Chip, IconButton } from '@shared/ui';
const { __http } = require('@testing/doubles/fetch.js');

type Sent = { type: string; payload: Record<string, unknown> };

const SETTLE_TICKS = 10;

async function settle(): Promise<void> {
  for (let i = 0; i < SETTLE_TICKS; i += 1) await Promise.resolve();
}

interface SentRequest {
  body: string;
}

function sent(): Sent[] {
  return __http.requests.map((request: SentRequest) => JSON.parse(request.body));
}

function Icon() {
  return <Text>icon</Text>;
}

beforeEach(async () => {
  await signInTestUser();
  __http.reply('POST /v1/discovery/events', { status: 202 });
});

describe('Button', () => {
  it('records one tapped action with the track id and calls onPress', async () => {
    const onPress = jest.fn();
    render(<Button label="Go" action="auth.sign_in" trackId="t1" onPress={onPress} testID="b" />);
    fireEvent.press(screen.getByTestId('b'));
    await waitFor(() => expect(sent()).toHaveLength(1));
    expect(sent()[0]).toMatchObject({
      type: 'user_action',
      payload: { action: 'auth.sign_in', outcome: 'tapped', track_id: 't1' },
    });
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('still records the tapped action when onPress throws', async () => {
    const onPress = jest.fn(() => {
      throw new Error('press failed');
    });
    render(<Button label="Go" action="auth.sign_in" onPress={onPress} testID="b" />);
    expect(() => fireEvent.press(screen.getByTestId('b'))).toThrow('press failed');
    await waitFor(() => expect(sent()).toHaveLength(1));
    expect(sent()[0]).toMatchObject({
      type: 'user_action',
      payload: { action: 'auth.sign_in', outcome: 'tapped' },
    });
  });

  it.each([{ disabled: true }, { loading: true }])('records nothing when %p', async (flags) => {
    const onPress = jest.fn();
    render(<Button label="Go" action="a.b" onPress={onPress} testID="b" {...flags} />);
    fireEvent.press(screen.getByTestId('b'));
    await settle();
    expect(__http.requests).toHaveLength(0);
    expect(onPress).not.toHaveBeenCalled();
  });
});

describe('IconButton', () => {
  it('records one tapped action then calls onPress', async () => {
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
    await waitFor(() => expect(sent()).toHaveLength(1));
    expect(sent()[0]).toMatchObject({
      type: 'user_action',
      payload: { action: 'playback.mini.play_pause', outcome: 'tapped', track_id: 't2' },
    });
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('records nothing when disabled', async () => {
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
    await settle();
    expect(__http.requests).toHaveLength(0);
    expect(onPress).not.toHaveBeenCalled();
  });
});

describe('Chip', () => {
  it('records one tapped action then calls onPress', async () => {
    const onPress = jest.fn();
    render(
      <Chip label="Downloaded" action="library.chip.downloaded" onPress={onPress} testID="c" />,
    );
    fireEvent.press(screen.getByTestId('c'));
    await waitFor(() => expect(sent()).toHaveLength(1));
    expect(sent()[0]).toMatchObject({
      type: 'user_action',
      payload: { action: 'library.chip.downloaded', outcome: 'tapped' },
    });
    expect(sent()[0]?.payload).not.toHaveProperty('track_id');
    expect(onPress).toHaveBeenCalledTimes(1);
  });
});
