import { expoTapDevice } from '@shared/ui/adapters/expoTapDevice';
import { createTapFeedback } from '@shared/ui/core/haptics/createTapFeedback';

export const tapFeedback = createTapFeedback(expoTapDevice);
