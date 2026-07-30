import { getPreviewUrl } from '../previewUrl';

describe('getPreviewUrl', () => {
  it.each([
    ['a valid string url', { preview_url: 'https://cdn.example.com/preview.mp3' }, 'https://cdn.example.com/preview.mp3'],
    ['an empty string', { preview_url: '' }, null],
    ['null', { preview_url: null }, null],
    ['undefined', { preview_url: undefined }, null],
    ['an absent key', {}, null],
    ['a number', { preview_url: 42 }, null],
    ['an object', { preview_url: { url: 'https://cdn.example.com/preview.mp3' } }, null],
    ['an array of strings', { preview_url: ['https://cdn.example.com/preview.mp3'] }, null],
    ['a boolean', { preview_url: true }, null],
  ] as [string, Record<string, unknown>, string | null][])('%s -> %j', (_label, extras, expected) => {
    expect(getPreviewUrl(extras)).toBe(expected);
  });
});
