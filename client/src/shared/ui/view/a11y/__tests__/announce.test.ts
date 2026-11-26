import { createAnnounce, type ScreenReader } from '@shared/ui';

const flush = (): Promise<void> => new Promise((resolve) => setImmediate(resolve));

type SpeakingReader = { reader: ScreenReader; spoken: string[]; asked: () => number };

function readerSaying(...enabled: boolean[]): SpeakingReader {
  const answers = [...enabled];
  const spoken: string[] = [];
  let asks = 0;
  return {
    spoken,
    asked: () => asks,
    reader: {
      isEnabled: () => {
        asks += 1;
        return Promise.resolve(answers.shift() ?? false);
      },
      speak: (message) => {
        spoken.push(message);
      },
    },
  };
}

describe('announce', () => {
  it('ignores an empty message', () => {
    const { reader, asked } = readerSaying(true);

    createAnnounce(reader)('');

    expect(asked()).toBe(0);
  });

  it('announces when the screen reader is on', async () => {
    const { reader, spoken } = readerSaying(true);

    createAnnounce(reader)('Saved');
    await flush();

    expect(spoken).toEqual(['Saved']);
  });

  it('stays silent when the screen reader is off', async () => {
    const { reader, spoken } = readerSaying(false);

    createAnnounce(reader)('Saved');
    await flush();

    expect(spoken).toEqual([]);
  });

  it('announces once across an on call followed by an off call', async () => {
    const { reader, spoken } = readerSaying(true, false);
    const announce = createAnnounce(reader);

    announce('First');
    announce('Second');
    await flush();

    expect(spoken).toEqual(['First']);
  });
});
