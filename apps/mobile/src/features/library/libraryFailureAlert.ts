import { Alert } from 'react-native';

import { failureTail, type LibraryFailure } from './state';

export function alertLibraryFailure(title: string, lead: string, failure: LibraryFailure): void {
  Alert.alert(title, `${lead} ${failureTail(failure)}`);
}
