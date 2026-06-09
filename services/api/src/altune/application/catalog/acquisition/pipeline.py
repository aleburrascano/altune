"""AcquisitionPipeline — sequential step runner with reverse rollback."""

from __future__ import annotations

from typing import TYPE_CHECKING, Protocol, runtime_checkable

if TYPE_CHECKING:
    from collections.abc import Sequence

    from altune.application.catalog.acquisition.context import AcquisitionContext


@runtime_checkable
class Step(Protocol):
    async def execute(self, ctx: AcquisitionContext) -> AcquisitionContext: ...
    async def rollback(self, ctx: AcquisitionContext) -> None: ...


class AcquisitionPipeline:
    def __init__(self, steps: Sequence[Step]) -> None:
        self._steps = steps

    async def run(self, ctx: AcquisitionContext) -> AcquisitionContext:
        completed: list[Step] = []
        try:
            for step in self._steps:
                ctx = await step.execute(ctx)
                completed.append(step)
        except Exception:
            for step in reversed(completed):
                await step.rollback(ctx)
            raise
        return ctx
