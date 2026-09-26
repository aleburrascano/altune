// Probe (#2819): the album's "Save N" pill, driven through its own props now it
// is its own unit. The body-level characterization pins its label and text; these
// pin the tap guard and the ticket's "no busy state" assumption.

import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { SaveAllPill } from '../ui/SaveAllPill';

function renderPill(unownedCount: number, saving: boolean): jest.Mock {
  const onSave = jest.fn();
  render(<SaveAllPill unownedCount={unownedCount} saving={saving} onSave={onSave} />);
  return onSave;
}

describe('SaveAllPill', () => {
  it('starts a save-all run when tapped with unowned tracks', () => {
    const onSave = renderPill(2, false);

    fireEvent.press(screen.getByTestId('detail-save-all'));

    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it('ignores taps while a save-all run is in flight', () => {
    const onSave = renderPill(2, true);

    fireEvent.press(screen.getByTestId('detail-save-all'));
    fireEvent.press(screen.getByTestId('detail-save-all'));

    expect(onSave).not.toHaveBeenCalled();
  });

  it('announces disabled but never busy while saving', () => {
    renderPill(2, true);

    const state = screen.getByTestId('detail-save-all').props.accessibilityState;
    expect(state?.disabled).toBe(true);
    expect(state?.busy).not.toBe(true);
  });

  it('renders nothing when every track is already owned', () => {
    renderPill(0, false);

    expect(screen.queryByTestId('detail-save-all')).toBeNull();
  });
});
