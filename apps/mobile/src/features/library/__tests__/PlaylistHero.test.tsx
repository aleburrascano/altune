import { render, screen } from '@testing-library/react-native';

import { PlaylistHero } from '../ui/PlaylistHero';

const playlist = {
  name: 'Late Night',
  track_count: 12,
  preview_artwork_urls: [],
};

function renderedMeta(totalDurationSeconds?: number): string {
  const total =
    totalDurationSeconds === undefined ? {} : { total_duration_seconds: totalDurationSeconds };
  render(
    <PlaylistHero
      playlist={{ ...playlist, ...total }}
      isEditing={false}
      editName={playlist.name}
      onEditNameChange={jest.fn()}
      onStartEditing={jest.fn()}
      onConfirmRename={jest.fn()}
      onPlay={jest.fn()}
      onShuffle={jest.fn()}
      onAddTracks={jest.fn()}
    />,
  );
  return screen.getByText(/^12 tracks/).props.children as string;
}

describe('playlist hero — a minutes remainder that rounds up to an hour carries', () => {
  it.each<[string, number, string]>([
    ['1h 59m 59s', 7199, '12 tracks · 2h 0m'],
    ['1h 59m 1s', 7141, '12 tracks · 2h 0m'],
    ['59m 59s', 3599, '12 tracks · 1h 0m'],
    ['59m 1s', 3541, '12 tracks · 1h 0m'],
  ])('carries the rounded-up minutes of %s into the hours', (_case, seconds, expected) => {
    expect(renderedMeta(seconds)).toBe(expected);
  });
});

describe('playlist hero — durations that need no carry keep their own minutes', () => {
  it.each<[number, string]>([
    [7200, '12 tracks · 2h 0m'],
    [3600, '12 tracks · 1h 0m'],
    [3661, '12 tracks · 1h 2m'],
    [3540, '12 tracks · 59m'],
    [90, '12 tracks · 2m'],
    [1, '12 tracks · 1m'],
  ])('renders %i seconds as "%s"', (seconds, expected) => {
    expect(renderedMeta(seconds)).toBe(expected);
  });
});

describe('playlist hero — an unknown total duration leaves the track count alone', () => {
  it.each<[string, number | undefined]>([
    ['missing', undefined],
    ['zero', 0],
  ])('drops the separator when the total is %s', (_case, seconds) => {
    expect(renderedMeta(seconds)).toBe('12 tracks');
  });
});
