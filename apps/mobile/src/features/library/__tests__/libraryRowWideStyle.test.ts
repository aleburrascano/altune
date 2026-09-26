import { lightTheme } from '@shared/ui';

import { libraryRowWideStyle } from '../ui/libraryRowWideStyle';

describe('libraryRowWideStyle', () => {
  it('paints a hover background when the row reports hovered', () => {
    const style = libraryRowWideStyle(lightTheme, false)({ hovered: true, pressed: false });

    expect(style).toEqual(
      expect.arrayContaining([expect.objectContaining({ backgroundColor: lightTheme.color.surface2 })]),
    );
  });

  it('rings the border in the accent color when the row reports focused', () => {
    const style = libraryRowWideStyle(lightTheme, false)({ focused: true, pressed: false });

    expect(style).toEqual(
      expect.arrayContaining([expect.objectContaining({ borderColor: lightTheme.color.accent })]),
    );
  });

  it('shows neither hover nor focus styling when the row is plain and idle', () => {
    const style = libraryRowWideStyle(lightTheme, false)({ pressed: false });

    expect(style).toEqual(
      expect.arrayContaining([expect.objectContaining({ borderColor: 'transparent' })]),
    );
    expect(style).not.toEqual(
      expect.arrayContaining([expect.objectContaining({ backgroundColor: lightTheme.color.surface2 })]),
    );
  });
});
