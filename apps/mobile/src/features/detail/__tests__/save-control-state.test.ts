import { saveControlLabel, saveControlState, saveControlText } from '../save-control-state';

describe('saveControlState', () => {
  it('is add when the server did not stamp ownership', () => {
    expect(saveControlState(null)).toBe('add');
  });

  it('is saving while acquisition is pending', () => {
    expect(saveControlState({ trackId: 't', acquisitionStatus: 'pending' })).toBe('saving');
  });

  it('is failed when acquisition failed', () => {
    expect(saveControlState({ trackId: 't', acquisitionStatus: 'failed' })).toBe('failed');
  });

  it('is ready once the audio is there', () => {
    expect(saveControlState({ trackId: 't', acquisitionStatus: 'ready' })).toBe('ready');
  });
});

describe('saveControlLabel', () => {
  it('names the track in every state', () => {
    expect(saveControlLabel('add', 'Song')).toBe('Save Song');
    expect(saveControlLabel('saving', 'Song')).toBe('Song downloading');
    expect(saveControlLabel('ready', 'Song')).toBe('Song in library');
    expect(saveControlLabel('failed', 'Song')).toBe('Retry saving Song');
  });
});

describe('saveControlText', () => {
  it('renders the short label', () => {
    expect(saveControlText('add')).toBe('Save');
    expect(saveControlText('saving')).toBe('Saving…');
    expect(saveControlText('ready')).toBe('Saved');
    expect(saveControlText('failed')).toBe('Retry');
  });
});
