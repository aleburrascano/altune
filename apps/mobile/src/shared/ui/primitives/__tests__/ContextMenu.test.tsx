import { fireEvent, render, screen } from '@testing-library/react-native';

import { ContextMenu } from '../ContextMenu';

describe('ContextMenu — disabled items', () => {
  it('does not run or close on a disabled item, and marks it disabled for assistive tech', () => {
    const onClose = jest.fn();
    const onPending = jest.fn();
    render(
      <ContextMenu
        visible
        onClose={onClose}
        items={[{ label: 'Re-acquiring…', disabled: true, onPress: onPending }]}
      />,
    );

    const item = screen.getByLabelText('Re-acquiring…');
    fireEvent.press(item);

    expect(onPending).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
    expect(item.props.accessibilityState).toEqual(expect.objectContaining({ disabled: true }));
  });

  it('closes and runs an enabled item', () => {
    const onClose = jest.fn();
    const onPress = jest.fn();
    render(<ContextMenu visible onClose={onClose} items={[{ label: 'View Details', onPress }]} />);

    fireEvent.press(screen.getByLabelText('View Details'));

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onPress).toHaveBeenCalledTimes(1);
  });
});
