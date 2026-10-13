import { faker } from '@faker-js/faker';
import { Factory } from 'fishery';

import { asTrackId } from '@shared/apiClient/ids';
import type { TrackResponse } from '@shared/apiClient/trackResponse';
import type { PlaybackContextValue } from '@shared/playback/playbackContextValue';

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

export function makeIdlePlayback(
  overrides: Partial<PlaybackContextValue> = {},
): PlaybackContextValue {
  return {
    status: 'idle',
    track: null,
    positionMs: 0,
    durationMs: 0,
    errorMessage: null,
    errorKind: null,
    play: jest.fn().mockResolvedValue(undefined),
    startQueue: jest.fn().mockResolvedValue(undefined),
    skipToQueueIndex: jest.fn().mockResolvedValue(undefined),
    reorderUpcoming: jest.fn().mockResolvedValue(undefined),
    appendToQueue: jest.fn().mockResolvedValue(undefined),
    insertNext: jest.fn().mockResolvedValue(undefined),
    skipNext: jest.fn().mockResolvedValue(undefined),
    skipPrevious: jest.fn().mockResolvedValue(undefined),
    removeQueueIndex: jest.fn().mockResolvedValue(undefined),
    pause: jest.fn(),
    resume: jest.fn(),
    seekTo: jest.fn(),
    setRate: jest.fn(),
    stop: jest.fn(),
    retry: jest.fn(),
    ...overrides,
  };
}
