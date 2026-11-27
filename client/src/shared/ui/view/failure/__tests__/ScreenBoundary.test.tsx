import { render, screen } from '@testing-library/react-native';
import type { ReactElement } from 'react';

import { ApiError } from '@shared/failure';
import { recordLogs } from '@testing/fakes/log/inMemoryLogger';
import { ScreenBoundary } from '@shared/ui';

const SERVER_FAULT = 500;
const CORRELATION_ID = 'abc123def4567890';

type BrokenProps = { error: Error };

function Broken({ error }: BrokenProps): ReactElement {
  throw error;
}

describe('ScreenBoundary', () => {
  it('shows the reference id of a failed request so a user can quote it', () => {
    recordLogs();
    const error = new ApiError(SERVER_FAULT, 'boom', undefined, CORRELATION_ID);

    render(
      <ScreenBoundary>
        <Broken error={error} />
      </ScreenBoundary>,
    );

    expect(screen.getByTestId('screen-error')).toBeTruthy();
    expect(screen.getByText(`Reference: ${CORRELATION_ID}`)).toBeTruthy();
  });

  it('shows no reference line for an error that carries no id', () => {
    recordLogs();

    render(
      <ScreenBoundary>
        <Broken error={new Error('render bug')} />
      </ScreenBoundary>,
    );

    expect(screen.getByTestId('screen-error')).toBeTruthy();
    expect(screen.queryByTestId('screen-error-ref')).toBeNull();
  });

  it('logs the render error at error level', () => {
    const logs = recordLogs();

    render(
      <ScreenBoundary>
        <Broken error={new Error('render bug')} />
      </ScreenBoundary>,
    );

    expect(logs.errors).toContainEqual([
      '[ScreenBoundary] render error',
      expect.any(Error),
      expect.any(String),
    ]);
  });
});
