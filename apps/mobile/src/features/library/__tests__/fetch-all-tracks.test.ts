import { fetchAllTracks, TRACKS_PAGE_SIZE } from '../fetch-all-tracks';
import { getTracks } from '@shared/api-client/tracks';
import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';

jest.mock('@shared/api-client/tracks', () => ({ getTracks: jest.fn() }));

const mockGetTracks = getTracks as jest.MockedFunction<typeof getTracks>;

const track = (id: string): TrackResponse => ({ id }) as TrackResponse;

const page = (ids: string[], hasMore: boolean, total = 0): ListTracksResponse => ({
  items: ids.map(track),
  total,
  limit: TRACKS_PAGE_SIZE,
  offset: 0,
  has_more: hasMore,
});

beforeEach(() => {
  mockGetTracks.mockReset();
});

it('makes a single request when the first page is the whole library', async () => {
  mockGetTracks.mockResolvedValueOnce(page(['a', 'b'], false, 2));

  const result = await fetchAllTracks();

  expect(result.items.map((t) => t.id)).toEqual(['a', 'b']);
  expect(mockGetTracks).toHaveBeenCalledTimes(1);
});

it('follows has_more and concatenates every page', async () => {
  mockGetTracks
    .mockResolvedValueOnce(page(['a'], true, 3))
    .mockResolvedValueOnce(page(['b'], true, 3))
    .mockResolvedValueOnce(page(['c'], false, 3));

  const result = await fetchAllTracks();

  expect(result.items.map((t) => t.id)).toEqual(['a', 'b', 'c']);
  expect(result.has_more).toBe(false);
});

it('advances the offset by the number of rows already collected', async () => {
  mockGetTracks
    .mockResolvedValueOnce(page(['a', 'b'], true, 3))
    .mockResolvedValueOnce(page(['c'], false, 3));

  await fetchAllTracks();

  expect(mockGetTracks).toHaveBeenNthCalledWith(1, { limit: TRACKS_PAGE_SIZE, offset: 0 });
  expect(mockGetTracks).toHaveBeenNthCalledWith(2, { limit: TRACKS_PAGE_SIZE, offset: 2 });
});

it('stops on an empty page even when has_more stays set', async () => {
  mockGetTracks
    .mockResolvedValueOnce(page(['a'], true, 1))
    .mockResolvedValueOnce(page([], true, 1));

  const result = await fetchAllTracks();

  expect(result.items.map((t) => t.id)).toEqual(['a']);
  expect(mockGetTracks).toHaveBeenCalledTimes(2);
});
