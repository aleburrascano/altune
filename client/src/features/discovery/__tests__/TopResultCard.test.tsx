import { StyleSheet } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { resultFixture } from '@features/discovery/__tests__/fixtures';
import { TopResultCard } from '@features/discovery/ui/TopResultCard';
import { darkTheme } from '@shared/ui/theme';

function renderCard() {
  render(<TopResultCard result={resultFixture({ title: 'Top Hit' })} onPress={jest.fn()} />);
}

function ringColor(): unknown {
  return StyleSheet.flatten(screen.getByTestId('discover-top-result-card').props.style).borderColor;
}

describe('TopResultCard', () => {
  it('rings the card in the accent colour while hovered, and clears it on hover-out', () => {
    renderCard();
    const pressable = screen.getByTestId('discover-top-result');
    expect(ringColor()).toBe('transparent');

    fireEvent(pressable, 'hoverIn');
    expect(ringColor()).toBe(darkTheme.color.accent);
    fireEvent(pressable, 'hoverOut');
    expect(ringColor()).toBe('transparent');
  });

  it('rings the card in the accent colour while focused, and clears it on blur', () => {
    renderCard();
    const pressable = screen.getByTestId('discover-top-result');

    fireEvent(pressable, 'focus');
    expect(ringColor()).toBe(darkTheme.color.accent);
    fireEvent(pressable, 'blur');
    expect(ringColor()).toBe('transparent');
  });
});
