import { render } from '@testing-library/react-native';

import { signInTestUser } from '@testing/signInTestUser';
import { applyKillSwitches } from '@shared/killSwitch/killSwitch';
import { Banner } from '@shared/ui/primitives/Banner';

const { __http } = require('@testing/doubles/fetch.js');

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));

type Sent = { type: string; payload: Record<string, unknown> };

const SETTLE_TICKS = 10;
const MESSAGE_CAP = 200;

async function settle(): Promise<void> {
  for (let i = 0; i < SETTLE_TICKS; i += 1) await Promise.resolve();
}

interface SentRequest {
  body: string;
}

function sent(): Sent[] {
  return __http.requests.map((request: SentRequest) => JSON.parse(request.body));
}

beforeEach(async () => {
  await signInTestUser();
  applyKillSwitches({ telemetry_enabled: true });
  __http.reply('POST /v1/discovery/events', { status: 202 });
});

describe('Banner failure_shown', () => {
  it('records the surface and message once on mount for a danger banner', async () => {
    render(
      <Banner tone="danger" surface="test.surface">
        Something broke
      </Banner>,
    );
    await settle();
    expect(sent()).toHaveLength(1);
    expect(sent()[0]).toMatchObject({
      type: 'failure_shown',
      payload: { surface: 'test.surface', message: 'Something broke' },
    });
  });

  it('does not re-record on a re-render with the same surface and message', async () => {
    const { rerender } = render(
      <Banner tone="danger" surface="test.surface">
        Something broke
      </Banner>,
    );
    rerender(
      <Banner tone="danger" surface="test.surface" testID="again">
        Something broke
      </Banner>,
    );
    await settle();
    expect(sent()).toHaveLength(1);
  });

  it('records again when the message changes', async () => {
    const { rerender } = render(
      <Banner tone="danger" surface="test.surface">
        First
      </Banner>,
    );
    rerender(
      <Banner tone="danger" surface="test.surface">
        Second
      </Banner>,
    );
    await settle();
    expect(sent().map((event) => event.payload.message)).toEqual(['First', 'Second']);
  });

  it('caps the message at 200 characters', async () => {
    render(
      <Banner tone="danger" surface="test.surface">
        {'x'.repeat(250)}
      </Banner>,
    );
    await settle();
    expect(sent()[0]?.payload).toMatchObject({ message: 'x'.repeat(MESSAGE_CAP) });
  });

  it('records nothing for an info banner', async () => {
    render(<Banner tone="info">Heads up</Banner>);
    render(<Banner>Default</Banner>);
    await settle();
    expect(__http.requests).toHaveLength(0);
  });
});
