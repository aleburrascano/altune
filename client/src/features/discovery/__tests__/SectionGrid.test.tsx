import { StyleSheet } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { resultFixture } from '@features/discovery/__tests__/fixtures';
import { SectionGrid } from '@features/discovery/ui/SectionGrid';
import { darkTheme } from '@shared/ui/theme/theme';

const BODY_ID = 'discover-grid-card-body-album-0';

function renderGrid() {
  render(
    <SectionGrid
      items={[
        resultFixture({ kind: 'album', title: 'A1' }),
        resultFixture({ kind: 'album', title: 'A2' }),
      ]}
      columns={4}
      onPress={jest.fn()}
    />,
  );
}

function ringColor(testID: string): unknown {
  return StyleSheet.flatten(screen.getByTestId(testID).props.style).borderColor;
}

describe('SectionGrid', () => {
  it('rings a card in the accent colour while hovered, and clears it on hover-out', () => {
    renderGrid();
    const card = screen.getByTestId('discover-grid-card-album-0');

    fireEvent(card, 'hoverIn');
    expect(ringColor(BODY_ID)).toBe(darkTheme.color.accent);
    expect(ringColor('discover-grid-card-body-album-1')).toBe('transparent');
    fireEvent(card, 'hoverOut');
    expect(ringColor(BODY_ID)).toBe('transparent');
  });

  it('rings a card in the accent colour while focused, and clears it on blur', () => {
    renderGrid();
    const card = screen.getByTestId('discover-grid-card-album-0');

    fireEvent(card, 'focus');
    expect(ringColor(BODY_ID)).toBe(darkTheme.color.accent);
    fireEvent(card, 'blur');
    expect(ringColor(BODY_ID)).toBe('transparent');
  });

  it('keeps the column slot width on the pressable', () => {
    renderGrid();
    expect(
      StyleSheet.flatten(screen.getByTestId('discover-grid-card-album-0').props.style).flexBasis,
    ).toBe('25%');
  });
});
