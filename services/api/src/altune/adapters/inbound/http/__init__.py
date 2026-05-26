"""HTTP inbound adapter — FastAPI routers per bounded context.

Routers added as packages here per bounded context (e.g., `catalog/router.py`).
Routers are thin shells: parse → call use case → serialize.
"""
