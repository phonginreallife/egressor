"""Client for FlowScope Go API."""

from typing import Any

import httpx
import structlog

from .config import settings
from .models import (
    AnalysisContext,
    AnomalySummary,
    CostSummary,
    FlowSummary,
    GraphStats,
)

logger = structlog.get_logger(__name__)


class FlowScopeClient:
    """Client to fetch data from FlowScope Go API."""

    def __init__(self) -> None:
        self.base_url = settings.api_url.rstrip("/")
        self.client = httpx.AsyncClient(timeout=30.0)

    async def close(self) -> None:
        """Close the HTTP client."""
        await self.client.aclose()

    async def get_graph_stats(self) -> GraphStats | None:
        """Fetch graph statistics."""
        try:
            resp = await self.client.get(f"{self.base_url}/api/v1/graph/stats")
            resp.raise_for_status()
            return GraphStats(**resp.json())
        except Exception as e:
            logger.error("failed_to_fetch_graph_stats", error=str(e))
            return None

    async def get_cost_summary(self) -> CostSummary | None:
        """Fetch cost summary."""
        try:
            resp = await self.client.get(f"{self.base_url}/api/v1/costs/summary")
            resp.raise_for_status()
            return CostSummary(**resp.json())
        except Exception as e:
            logger.error("failed_to_fetch_cost_summary", error=str(e))
            return None

    async def get_top_flows(self, n: int = 10) -> list[FlowSummary]:
        """Fetch top flows by volume."""
        try:
            resp = await self.client.get(
                f"{self.base_url}/api/v1/graph/top-edges",
                params={"n": n},
            )
            resp.raise_for_status()
            data = resp.json()
            return [
                FlowSummary(
                    source=f["source"],
                    destination=f["target"],
                    transfer_type=f["transfer_type"],
                    total_bytes=f["total_bytes"],
                    total_events=f.get("total_events", 0),
                    cost_usd=f.get("cost_usd", 0),
                )
                for f in data
            ]
        except Exception as e:
            logger.error("failed_to_fetch_top_flows", error=str(e))
            return []

    async def get_active_anomalies(self) -> list[AnomalySummary]:
        """Fetch active anomalies."""
        try:
            resp = await self.client.get(f"{self.base_url}/api/v1/anomalies/active")
            resp.raise_for_status()
            data = resp.json()
            if not data:
                return []
            return [AnomalySummary(**a) for a in data]
        except Exception as e:
            logger.error("failed_to_fetch_anomalies", error=str(e))
            return []

    async def get_anomaly(self, anomaly_id: str) -> AnomalySummary | None:
        """Fetch a specific anomaly."""
        try:
            resp = await self.client.get(f"{self.base_url}/api/v1/anomalies/{anomaly_id}")
            resp.raise_for_status()
            return AnomalySummary(**resp.json())
        except Exception as e:
            logger.error("failed_to_fetch_anomaly", error=str(e), anomaly_id=anomaly_id)
            return None

    async def get_related_flows(self, service: str) -> list[dict[str, Any]]:
        """Fetch flows related to a service."""
        try:
            resp = await self.client.get(
                f"{self.base_url}/api/v1/graph/service/{service}",
                params={"depth": 1},
            )
            resp.raise_for_status()
            data = resp.json()
            return data.get("edges", [])
        except Exception as e:
            logger.error("failed_to_fetch_related_flows", error=str(e), service=service)
            return []

    async def get_baseline(self, flow_key: str) -> dict[str, Any] | None:
        """Fetch baseline for a flow."""
        try:
            resp = await self.client.get(f"{self.base_url}/api/v1/baselines/{flow_key}")
            resp.raise_for_status()
            return resp.json()
        except Exception as e:
            logger.error("failed_to_fetch_baseline", error=str(e), flow_key=flow_key)
            return None

    async def build_analysis_context(self, time_range_hours: int = 24) -> AnalysisContext:
        """Build complete analysis context from FlowScope data."""
        graph_stats = await self.get_graph_stats()
        cost_summary = await self.get_cost_summary()
        top_flows = await self.get_top_flows(10)
        anomalies = await self.get_active_anomalies()

        return AnalysisContext(
            graph_stats=graph_stats,
            cost_summary=cost_summary,
            top_flows=top_flows,
            active_anomalies=anomalies,
            time_range_hours=time_range_hours,
        )
