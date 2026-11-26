import { reloadWidget, writeCoverFromUrl, writeSnapshot } from '@modules/altune-widget';

import type { WidgetBridge } from '@features/player/ports/widgetBridge';

export const altuneWidgetBridge: WidgetBridge = { writeSnapshot, writeCoverFromUrl, reloadWidget };
