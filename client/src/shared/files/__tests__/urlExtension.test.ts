import { extFromUrl } from '@shared/files/urlExtension';

describe('extFromUrl', () => {
  it.each<[string, string]>([
    ['https://cdn.example/a/track.flac?sig=v1.2', '.flac'],
    ['https://cdn.example/a.v2/track?sig=v1.2', '.mp3'],
    ['https://cdn.example/a/track.', '.mp3'],
    ['', '.mp3'],
  ])('%s -> %s', (url, expected) => {
    expect(extFromUrl(url)).toBe(expected);
  });
});
