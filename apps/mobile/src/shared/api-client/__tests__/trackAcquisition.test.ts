import { expectType, type IsExactlyAssignable, type Not } from '../../../../jest/typeAssertions';
import {
  acquisitionOf,
  toFailed,
  toPending,
  toReady,
  toTrackStatus,
  type TrackStatus,
} from '../trackAcquisition';

describe('track acquisition transitions (#933)', () => {
  it('builds each state as the whole triple, with failure text only on failed', () => {
    expect(toPending()).toEqual({
      acquisition_status: 'pending',
      failure_reason: null,
      failure_message: null,
    });
    expect(toReady()).toEqual({
      acquisition_status: 'ready',
      failure_reason: null,
      failure_message: null,
    });
    expect(toFailed('no_source', 'No source found')).toEqual({
      acquisition_status: 'failed',
      failure_reason: 'no_source',
      failure_message: 'No source found',
    });
  });

  it('acquisitionOf detaches a whole triple for every status', () => {
    expect(acquisitionOf({ acquisition_status: 'pending', failure_reason: null })).toEqual(
      toPending(),
    );
    expect(acquisitionOf({ acquisition_status: 'ready', failure_reason: null })).toEqual(toReady());
    expect(
      acquisitionOf({
        acquisition_status: 'failed',
        failure_reason: 'no_source',
        failure_message: 'No source found',
      }),
    ).toEqual(toFailed('no_source', 'No source found'));
  });

  it('acquisitionOf fills an omitted failure_message on a failed track with null', () => {
    expect(acquisitionOf({ acquisition_status: 'failed', failure_reason: 'no_source' })).toEqual(
      toFailed('no_source', null),
    );
  });
});

describe('the per-track status the store keeps (#1758)', () => {
  it('drops the reason and keeps the failure text only on failed', () => {
    expect(toTrackStatus(toPending())).toEqual({
      acquisitionStatus: 'pending',
      failureMessage: null,
    });
    expect(toTrackStatus(toReady())).toEqual({ acquisitionStatus: 'ready', failureMessage: null });
    expect(toTrackStatus(toFailed('no_source', 'No source found'))).toEqual({
      acquisitionStatus: 'failed',
      failureMessage: 'No source found',
    });
  });

  it('refuses failure text on a status that cannot carry one', () => {
    const settled = { acquisitionStatus: 'ready', failureMessage: 'No source found' };

    expectType<
      Not<IsExactlyAssignable<{ acquisitionStatus: 'ready'; failureMessage: string }, TrackStatus>>
    >();
    expect(toTrackStatus(toReady())).not.toEqual(settled);
  });
});
