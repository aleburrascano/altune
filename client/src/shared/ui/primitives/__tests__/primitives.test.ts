import * as primitives from '@shared/ui/primitives/index';
import { ActionSheet } from '@shared/ui/primitives/ActionSheet';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { SearchBar } from '@shared/ui/primitives/SearchBar';
import { TextField } from '@shared/ui/primitives/TextField';
import type {
  ActionSheetOption,
  ArtworkProps,
  MenuAnchor,
  SearchBarProps,
  TextFieldProps,
} from '@shared/ui/primitives/index';

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
