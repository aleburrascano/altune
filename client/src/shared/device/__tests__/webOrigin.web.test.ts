import { webOrigin } from '../webOrigin.web';

afterEach(() => {
  Reflect.deleteProperty(globalThis, 'window');
});

describe('webOrigin (web)', () => {
  it('returns the window origin', () => {
    Object.defineProperty(globalThis, 'window', {
      value: { location: { origin: 'https://altune.example' } },
      configurable: true,
    });
    expect(webOrigin()).toBe('https://altune.example');
  });

  it('returns null when there is no window', () => {
    expect(webOrigin()).toBeNull();
  });

  it('returns null when window has no location', () => {
    Object.defineProperty(globalThis, 'window', { value: {}, configurable: true });
    expect(webOrigin()).toBeNull();
  });
});
