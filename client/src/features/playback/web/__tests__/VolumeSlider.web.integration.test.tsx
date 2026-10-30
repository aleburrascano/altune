import { act } from 'react';
import { createRoot } from 'react-dom/client';

import { WIDE_LAYOUT_MIN_WIDTH } from '@shared/ui/layout/breakpoints';

import { toVolume, usePlaybackVolumeStore } from '@features/playback/web/playbackVolume';
import { VolumeSlider } from '@features/playback/web/VolumeSlider';

const CHOSEN = 0.5;
const NARROW_WIDTH = WIDE_LAYOUT_MIN_WIDTH - 1;

let container: HTMLDivElement;
let root: ReturnType<typeof createRoot>;

function setWindowWidth(width: number): void {
  jest.spyOn(document.documentElement, 'clientWidth', 'get').mockReturnValue(width);
  act(() => {
    window.dispatchEvent(new Event('resize'));
  });
}

function renderSlider(): void {
  act(() => {
    root.render(<VolumeSlider />);
  });
}

function slider(): HTMLInputElement | null {
  return container.querySelector<HTMLInputElement>('input[type="range"][aria-label="Volume"]');
}

function moveSliderTo(input: HTMLInputElement, value: number): void {
  const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
  act(() => {
    setValue?.call(input, String(value));
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

beforeEach(() => {
  container = document.createElement('div');
  document.body.append(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root.unmount();
  });
  container.remove();
  jest.restoreAllMocks();
  usePlaybackVolumeStore.setState({ volume: toVolume(1) });
});

describe('VolumeSlider on web', () => {
  it('shows the slider on a wide window', () => {
    setWindowWidth(WIDE_LAYOUT_MIN_WIDTH);

    renderSlider();

    expect(slider()).not.toBeNull();
  });

  it('hides the slider on a narrow window, where the phone buttons set volume', () => {
    setWindowWidth(NARROW_WIDTH);

    renderSlider();

    expect(slider()).toBeNull();
  });

  it('writes the chosen value to the volume store', () => {
    setWindowWidth(WIDE_LAYOUT_MIN_WIDTH);
    renderSlider();

    moveSliderTo(slider() as HTMLInputElement, CHOSEN);

    expect(usePlaybackVolumeStore.getState().volume).toBe(CHOSEN);
  });
});
