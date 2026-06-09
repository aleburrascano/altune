"""AcquisitionPipeline — step runner with rollback on failure."""

from __future__ import annotations

from dataclasses import dataclass, field

import pytest
from altune.application.catalog.acquisition.context import AcquisitionContext
from altune.application.catalog.acquisition.pipeline import AcquisitionPipeline


@dataclass
class _RecordingStep:
    name: str
    executed: list[str] = field(default_factory=list)
    rolled_back: list[str] = field(default_factory=list)
    should_fail: bool = False

    async def execute(self, ctx: AcquisitionContext) -> AcquisitionContext:
        self.executed.append(self.name)
        if self.should_fail:
            msg = f"{self.name} failed"
            raise RuntimeError(msg)
        return ctx

    async def rollback(self, ctx: AcquisitionContext) -> None:
        self.rolled_back.append(self.name)


@pytest.mark.unit
async def test_pipeline_executes_steps_in_order() -> None:
    log: list[str] = []
    s1 = _RecordingStep("s1", executed=log)
    s2 = _RecordingStep("s2", executed=log)
    s3 = _RecordingStep("s3", executed=log)
    pipeline = AcquisitionPipeline([s1, s2, s3])

    await pipeline.run(AcquisitionContext())

    assert log == ["s1", "s2", "s3"]


@pytest.mark.unit
async def test_pipeline_rolls_back_on_failure_in_reverse() -> None:
    rollback_log: list[str] = []
    s1 = _RecordingStep("s1", rolled_back=rollback_log)
    s2 = _RecordingStep("s2", rolled_back=rollback_log)
    s3 = _RecordingStep("s3", should_fail=True, rolled_back=rollback_log)
    pipeline = AcquisitionPipeline([s1, s2, s3])

    with pytest.raises(RuntimeError, match="s3 failed"):
        await pipeline.run(AcquisitionContext())

    assert rollback_log == ["s2", "s1"]


@pytest.mark.unit
async def test_pipeline_does_not_rollback_failed_step() -> None:
    rollback_log: list[str] = []
    s1 = _RecordingStep("s1", rolled_back=rollback_log)
    s2 = _RecordingStep("s2", should_fail=True, rolled_back=rollback_log)
    pipeline = AcquisitionPipeline([s1, s2])

    with pytest.raises(RuntimeError):
        await pipeline.run(AcquisitionContext())

    assert rollback_log == ["s1"]


@pytest.mark.unit
async def test_pipeline_returns_final_context() -> None:
    s1 = _RecordingStep("s1")
    pipeline = AcquisitionPipeline([s1])
    ctx = AcquisitionContext()

    result = await pipeline.run(ctx)

    assert result is ctx
