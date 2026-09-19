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
    ['a plain http url', { preview_url: 'http://cdn.example.com/preview.mp3' }, null],
    ['an upper-case scheme', { preview_url: 'HTTPS://cdn.example.com/preview.mp3' }, 'HTTPS://cdn.example.com/preview.mp3'],
    ['a file url', { preview_url: 'file:///data/data/app.altune/files/token.json' }, null],
    ['a content url', { preview_url: 'content://com.android.contacts/contacts/1' }, null],
    ['a javascript url', { preview_url: 'javascript:alert(1)' }, null],
    ['a data url', { preview_url: 'data:audio/mpeg;base64,SUQz' }, null],
    ['a scheme-relative url', { preview_url: '//cdn.example.com/preview.mp3' }, null],
    ['a bare path', { preview_url: '/preview.mp3' }, null],
    ['a scheme with no host', { preview_url: 'https://' }, null],
    ['a scheme without its slashes', { preview_url: 'https:cdn.example.com/preview.mp3' }, null],
    ['a url with a leading space', { preview_url: ' https://cdn.example.com/preview.mp3' }, null],
    ['a url with an embedded newline', { preview_url: 'https://cdn.example.com/a\nfile:///etc/passwd' }, null],
  ] as [string, Record<string, unknown>, string | null][])('%s -> %j', (_label, extras, expected) => {
    expect(getPreviewUrl(extras)).toBe(expected);
  });
});
