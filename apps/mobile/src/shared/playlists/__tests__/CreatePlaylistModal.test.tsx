import { render, screen, fireEvent } from '@testing-library/react-native';

import { CreatePlaylistModal } from '../CreatePlaylistModal';

type RenderOptions = {
  visible?: boolean;
  loading?: boolean;
};

function renderModal(options: RenderOptions = {}) {
  const onCreate = jest.fn();
  const onClose = jest.fn();
  const { visible = true, loading } = options;
  render(
    <CreatePlaylistModal
      visible={visible}
      onClose={onClose}
      onCreate={onCreate}
      {...(loading === undefined ? {} : { loading })}
    />,
  );
  return { onCreate, onClose };
}

describe('CreatePlaylistModal(): the confirm button is disabled exactly where name.trim() is empty (Table)', () => {
  it.each([
    ['an empty name disables it', '', true],
    ['a whitespace-only name disables it (trims to zero characters)', '   ', true],
    ['a single character padded with whitespace enables it (trims to exactly one)', ' a ', false],
    ['an ordinary name enables it', 'Road Trip Mix', false],
  ])('%s', (_label, value, expectedDisabled) => {
    renderModal();
    fireEvent.changeText(screen.getByTestId('create-playlist-input'), value);

    expect(screen.getByTestId('create-playlist-confirm').props.accessibilityState.disabled).toBe(
      expectedDisabled,
    );
  });
});

describe('CreatePlaylistModal(): pressing Create with a name the button allows through', () => {
  it('calls onCreate with the trimmed name and clears the field', () => {
    const { onCreate } = renderModal();
    fireEvent.changeText(screen.getByTestId('create-playlist-input'), '  Road Trip Mix  ');

    fireEvent.press(screen.getByTestId('create-playlist-confirm'));

    expect(onCreate).toHaveBeenCalledWith('Road Trip Mix');
    expect(onCreate).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('create-playlist-input').props.value).toBe('');
  });
});

describe('CreatePlaylistModal(): onSubmitEditing reaches handleCreate without passing through the Create button (Concurrency / ordering)', () => {
  it('a whitespace-only name submitted from the keyboard creates nothing and leaves the field untouched', () => {
    const { onCreate } = renderModal();
    const input = screen.getByTestId('create-playlist-input');
    fireEvent.changeText(input, '   ');

    fireEvent(input, 'submitEditing');

    expect(onCreate).not.toHaveBeenCalled();
    expect(screen.getByTestId('create-playlist-input').props.value).toBe('   ');
  });

  it('an ordinary name submitted from the keyboard creates a playlist and clears the field, same as the button', () => {
    const { onCreate } = renderModal();
    const input = screen.getByTestId('create-playlist-input');
    fireEvent.changeText(input, 'Road Trip Mix');

    fireEvent(input, 'submitEditing');

    expect(onCreate).toHaveBeenCalledWith('Road Trip Mix');
    expect(screen.getByTestId('create-playlist-input').props.value).toBe('');
  });

  it('the Create button ignores a press while a create is already in flight, even with a valid name', () => {
    const { onCreate } = renderModal({ loading: true });
    fireEvent.changeText(screen.getByTestId('create-playlist-input'), 'Road Trip Mix');

    fireEvent.press(screen.getByTestId('create-playlist-confirm'));

    expect(onCreate).not.toHaveBeenCalled();
  });

  it(
    'the keyboard submit path is gated by loading too, so it cannot fire a second onCreate while the button is disabled and spinning',
    () => {
      const { onCreate } = renderModal({ loading: true });
      const input = screen.getByTestId('create-playlist-input');
      fireEvent.changeText(input, 'Road Trip Mix');

      fireEvent(input, 'submitEditing');

      expect(onCreate).not.toHaveBeenCalled();
    },
  );
});

describe('CreatePlaylistModal(): handleClose resets the field and calls onClose, from every close affordance', () => {
  it('the Cancel button', () => {
    const { onClose } = renderModal();
    fireEvent.changeText(screen.getByTestId('create-playlist-input'), 'Draft name');

    fireEvent.press(screen.getByRole('button', { name: 'Cancel' }));

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('create-playlist-input').props.value).toBe('');
  });

  it('the backdrop, exposed to assistive tech as a labelled "Close" button', () => {
    const { onClose } = renderModal();
    fireEvent.changeText(screen.getByTestId('create-playlist-input'), 'Draft name');

    fireEvent.press(screen.getByRole('button', { name: 'Close' }));

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('create-playlist-input').props.value).toBe('');
  });

  it('the Android back button, via onRequestClose', () => {
    const { onClose } = renderModal();
    fireEvent.changeText(screen.getByTestId('create-playlist-input'), 'Draft name');

    fireEvent(screen.getByTestId('create-playlist-modal'), 'requestClose');

    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('create-playlist-input').props.value).toBe('');
  });
});

describe('CreatePlaylistModal(): the playlist name field carries a 100-character cap', () => {
  it('sets maxLength to 100 on the name input, the only observable form of the cap under fireEvent', () => {
    renderModal();

    expect(screen.getByTestId('create-playlist-input').props.maxLength).toBe(100);
  });
});

describe('CreatePlaylistModal(): the loading prop', () => {
  it('defaults to false, leaving the confirm button idle and enabled once a name is entered', () => {
    renderModal();
    fireEvent.changeText(screen.getByTestId('create-playlist-input'), 'Road Trip Mix');

    const confirm = screen.getByTestId('create-playlist-confirm');
    expect(confirm.props.accessibilityState.busy).toBe(false);
    expect(confirm.props.accessibilityState.disabled).toBe(false);
    expect(screen.getByText('Create')).toBeTruthy();
  });

  it('when true, marks the confirm button busy and disabled regardless of name validity', () => {
    renderModal({ loading: true });
    fireEvent.changeText(screen.getByTestId('create-playlist-input'), 'Road Trip Mix');

    const confirm = screen.getByTestId('create-playlist-confirm');
    expect(confirm.props.accessibilityState.busy).toBe(true);
    expect(confirm.props.accessibilityState.disabled).toBe(true);
    expect(screen.queryByText('Create')).toBeNull();
  });
});
