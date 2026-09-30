import { render } from '@testing-library/react-native';

import { recordFailureShown } from '../../../telemetry/userTelemetry';
import { Banner } from '../Banner';

jest.mock('../../../telemetry/userTelemetry', () => ({ recordFailureShown: jest.fn() }));

const record = jest.mocked(recordFailureShown);

beforeEach(() => record.mockClear());

describe('Banner failure_shown', () => {
  it('records the surface and message once on mount for a danger banner', () => {
    render(
      <Banner tone="danger" surface="test.surface">
        Something broke
      </Banner>,
    );
    expect(record).toHaveBeenCalledTimes(1);
    expect(record).toHaveBeenCalledWith({ surface: 'test.surface', message: 'Something broke' });
  });

  it('does not re-record on a re-render with the same surface and message', () => {
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
    expect(record).toHaveBeenCalledTimes(1);
  });

  it('records again when the message changes', () => {
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
    expect(record).toHaveBeenCalledTimes(2);
  });

  it('caps the message at 200 characters', () => {
    render(
      <Banner tone="danger" surface="test.surface">
        {'x'.repeat(250)}
      </Banner>,
    );
    expect(record).toHaveBeenCalledWith({ surface: 'test.surface', message: 'x'.repeat(200) });
  });

  it('records nothing for an info banner', () => {
    render(<Banner tone="info">Heads up</Banner>);
    render(<Banner>Default</Banner>);
    expect(record).not.toHaveBeenCalled();
  });
});
