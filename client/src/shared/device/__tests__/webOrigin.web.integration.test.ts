import { webOrigin } from '@shared/device';

function removeWindow(): void {
  Reflect.deleteProperty(globalThis, 'window');
}

beforeEach(removeWindow);
afterEach(removeWindow);

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

  it('returns null when the origin is empty', () => {
    Object.defineProperty(globalThis, 'window', {
      value: { location: { origin: '' } },
      configurable: true,
    });
    expect(webOrigin()).toBeNull();
  });

  it('returns null when window has no location', () => {
    Object.defineProperty(globalThis, 'window', { value: {}, configurable: true });
    expect(webOrigin()).toBeNull();
  });
});
