import { faker } from '@faker-js/faker';
import type { Session } from '@supabase/supabase-js';
import { Factory } from 'fishery';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/trackResponse';

const REFERENCE_DATE = new Date('2026-01-01T00:00:00Z');

export const trackResponseFactory = Factory.define<TrackResponse>(() => ({
  id: asTrackId(faker.string.uuid()),
  title: faker.music.songName(),
  artist: faker.person.fullName(),
  album: faker.lorem.words(2),
  duration_seconds: faker.number.int({ min: 60, max: 600 }),
  added_at: faker.date.past({ refDate: REFERENCE_DATE }).toISOString(),
  acquisition_status: 'ready',
  artwork_url: faker.image.url(),
  failure_reason: null,
  year: faker.number.int({ min: 1960, max: 2025 }),
  genre: faker.music.genre(),
  track_number: faker.number.int({ min: 1, max: 20 }),
  album_artist: faker.person.fullName(),
  isrc: faker.string.alphanumeric({ length: 12, casing: 'upper' }),
  audio_ref: null,
}));

export const sessionFactory = Factory.define<Session>(() => {
  const userId = faker.string.uuid();
  return {
    access_token: faker.string.alphanumeric(32),
    refresh_token: faker.string.alphanumeric(32),
    expires_in: 3600,
    token_type: 'bearer',
    user: {
      id: userId,
      aud: 'authenticated',
      app_metadata: {},
      user_metadata: {},
      created_at: faker.date.past({ refDate: REFERENCE_DATE }).toISOString(),
    },
  };
});
