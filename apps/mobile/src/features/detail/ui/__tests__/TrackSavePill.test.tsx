import React from 'react';
import { fireEvent, render, screen, within } from '@testing-library/react-native';

import type { SaveState } from '../../save-control-state';
import { TrackSavePill } from '../TrackSavePill';

const TITLE = 'Midnight City';

function renderPill(save: SaveState): jest.Mock {
  const onSave = jest.fn();
  render(<TrackSavePill save={save} onSave={onSave} title={TITLE} />);
  return onSave;
}

function pill(): ReturnType<typeof screen.getByRole> {
  return screen.getByRole('button');
}

function pillIsDisabled(): boolean {
  return pill().props.accessibilityState?.disabled === true;
}

describe('TrackSavePill labels', () => {
  it('offers to save an unsaved track', () => {
    renderPill('add');

    expect(screen.getByLabelText(`Save ${TITLE}`)).toBeTruthy();
    expect(within(pill()).getByText('Save')).toBeTruthy();
    expect(pillIsDisabled()).toBe(false);
  });

  it('shows the plain save affordance, disabled, for a track with no known artist', () => {
    renderPill('disabled');

    expect(screen.getByLabelText(`Save ${TITLE}`)).toBeTruthy();
    expect(within(pill()).getByText('Save')).toBeTruthy();
    expect(pillIsDisabled()).toBe(true);
  });

  it('announces a download in progress while saving', () => {
    renderPill('saving');

    expect(screen.getByLabelText(`${TITLE} downloading`)).toBeTruthy();
    expect(within(pill()).getByText('Saving…')).toBeTruthy();
    expect(pillIsDisabled()).toBe(true);
  });

  it('announces library membership once saved', () => {
    renderPill('ready');

    expect(screen.getByLabelText(`${TITLE} in library`)).toBeTruthy();
    expect(within(pill()).getByText('Saved')).toBeTruthy();
    expect(pillIsDisabled()).toBe(true);
  });

  it('offers a retry after a failed save', () => {
    renderPill('failed');

    expect(screen.getByLabelText(`Retry saving ${TITLE}`)).toBeTruthy();
    expect(within(pill()).getByText('Retry')).toBeTruthy();
    expect(pillIsDisabled()).toBe(false);
  });

  it('names a permanent refusal and offers no retry', () => {
    renderPill('rejected');

    expect(screen.getByLabelText(`Couldn't save ${TITLE}`)).toBeTruthy();
    expect(within(pill()).getByText("Can't save")).toBeTruthy();
    expect(pillIsDisabled()).toBe(true);
  });
});

describe('TrackSavePill tap guard', () => {
  it('saves when tapped on an unsaved track', () => {
    const onSave = renderPill('add');

    fireEvent.press(pill());

    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it('retries when tapped after a failed save', () => {
    const onSave = renderPill('failed');

    fireEvent.press(pill());

    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it.each<SaveState>(['disabled', 'saving', 'ready', 'rejected'])(
    'ignores a tap while %s',
    (save) => {
      const onSave = renderPill(save);

      fireEvent.press(pill());
      fireEvent.press(pill());

      expect(onSave).not.toHaveBeenCalled();
    },
  );
});
