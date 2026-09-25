import * as primitives from '../index';
import { ActionSheet } from '../ActionSheet';
import { Artwork } from '../Artwork';
import { SearchBar } from '../SearchBar';
import { TextField } from '../TextField';
import type {
  ActionSheetOption,
  ArtworkProps,
  MenuAnchor,
  SearchBarProps,
  TextFieldProps,
} from '../index';

// Type-level check: these compile only if the barrel re-exports the types.
type BarrelTypes = [ActionSheetOption, ArtworkProps, MenuAnchor, SearchBarProps, TextFieldProps];
const typesReachable: BarrelTypes | null = null;

describe('primitives barrel', () => {
  it('re-exports the same component bindings as the deep modules', () => {
    expect(primitives.ActionSheet).toBe(ActionSheet);
    expect(primitives.Artwork).toBe(Artwork);
    expect(primitives.SearchBar).toBe(SearchBar);
    expect(primitives.TextField).toBe(TextField);
    expect(typesReachable).toBeNull();
  });
});
