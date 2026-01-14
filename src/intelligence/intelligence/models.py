"""Data models for the Intelligence Service."""

from datetime import datetime
from typing import Any

from pydantic import BaseModel, Field


class ServiceIdentity(BaseModel):
    """Service identity from FlowScope."""

    namespace: str
    name: str
    kind: str = "Deployment"


class FlowSummary(BaseModel):
    """Summary of a transfer flow."""

    source: str
    destination: str
    transfer_type: str
    total_bytes: int
    total_events: int
    cost_usd: float = 0.0


class AnomalySummary(BaseModel):
    """Summary of an anomaly."""

    id: str
    type: str
    severity: str
    source_service: str
    destination_service: str | None = None
    current_value: float
    baseline_value: float
    deviation: float
    cost_impact_usd: float
    detected_at: datetime
    ai_summary: str | None = None


class CostSummary(BaseModel):
    """Cost summary from FlowScope."""

    total_cost_usd: float
    egress_cost_usd: float
    cross_region_cost_usd: float
    cross_az_cost_usd: float
    total_bytes: int
    by_namespace: dict[str, float] = Field(default_factory=dict)
    by_service: dict[str, float] = Field(default_factory=dict)


class GraphStats(BaseModel):
    """Transfer graph statistics."""

    total_nodes: int
    total_external_nodes: int
    total_edges: int
    total_bytes: int
    egress_bytes: int
    cross_region_bytes: int


class AnalysisContext(BaseModel):
    """Context prepared for Claude analysis."""

    graph_stats: GraphStats | None = None
    cost_summary: CostSummary | None = None
    top_flows: list[FlowSummary] = Field(default_factory=list)
    active_anomalies: list[AnomalySummary] = Field(default_factory=list)
    time_range_hours: int = 24


class AnalysisResult(BaseModel):
    """Result from Claude analysis."""

    summary: str
    key_findings: list[str] = Field(default_factory=list)
    cost_insights: list[str] = Field(default_factory=list)
    recommendations: list[str] = Field(default_factory=list)
    risk_assessment: str | None = None
    raw_response: str | None = None


class InvestigationRequest(BaseModel):
    """Request to investigate an anomaly."""

    anomaly_id: str
    include_related_flows: bool = True
    include_baseline: bool = True


class QuestionRequest(BaseModel):
    """Natural language question request."""

    question: str
    context_hours: int = 24


class CostExplainRequest(BaseModel):
    """Request to explain cost changes."""

    current_period_hours: int = 24
    comparison_period_hours: int = 24


class Optimization(BaseModel):
    """Suggested optimization."""

    description: str
    estimated_monthly_savings_usd: float
    difficulty: str  # low, medium, high
    steps: list[str] = Field(default_factory=list)
    affected_services: list[str] = Field(default_factory=list)
