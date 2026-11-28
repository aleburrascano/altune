import * as Linking from 'expo-linking';

import { createLinkEvents } from '@features/signIn/adapters/native/linkEvents';
import type { LinkEvents } from '@features/signIn/ports/ports';

export const nativeLinkEvents: LinkEvents = createLinkEvents(Linking);
