import { canPlay } from '@shared/playback/canPlay';

import { asAcquisitionStatus } from '../queueStateWire';

describe('asAcquisitionStatus', () => {
  it('keeps the known statuses', () => {
    expect(asAcquisitionStatus('ready', 'x')).toBe('ready');
    expect(asAcquisitionStatus('pending', 'x')).toBe('pending');
    expect(asAcquisitionStatus('failed', 'x')).toBe('failed');
  });

  it('carries an unrecognized status as not playable instead of throwing', () => {
    const status = asAcquisitionStatus('transcoding', 'x');
    expect(status).toBe('failed');
    expect(canPlay(status)).toBe(false);
  });

  it('rejects a non-string status', () => {
    expect(() => asAcquisitionStatus(3, 'x')).toThrow();
  });
});
