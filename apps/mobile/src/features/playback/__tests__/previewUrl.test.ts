import { getPreviewUrl } from '../helpers/previewUrl';

describe('getPreviewUrl', () => {
  it('returns the URL when preview_url is a non-empty string', () => {
    expect(getPreviewUrl({ preview_url: 'https://cdn.example.com/preview.mp3' })).toBe(
      'https://cdn.example.com/preview.mp3',
    );
  });

  it('returns null when preview_url is null', () => {
    expect(getPreviewUrl({ preview_url: null })).toBeNull();
  });

  it('returns null when preview_url is undefined', () => {
    expect(getPreviewUrl({})).toBeNull();
  });

  it('returns null when preview_url is an empty string', () => {
    expect(getPreviewUrl({ preview_url: '' })).toBeNull();
  });

  it('returns null when preview_url is a number', () => {
    expect(getPreviewUrl({ preview_url: 42 })).toBeNull();
  });

  it('returns null when extras is empty', () => {
    expect(getPreviewUrl({})).toBeNull();
  });
});
