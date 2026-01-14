"""FastAPI application for the Intelligence Service."""

from contextlib import asynccontextmanager
from typing import AsyncGenerator

import structlog
from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware

from .claude import ClaudeClient
from .config import settings
from .egressor_client import FlowScopeClient
from .models import (
    AnalysisResult,
    CostExplainRequest,
    InvestigationRequest,
    Optimization,
    QuestionRequest,
)

logger = structlog.get_logger(__name__)

# Global clients
egressor_client: FlowScopeClient | None = None
claude_client: ClaudeClient | None = None


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncGenerator[None, None]:
    """Application lifespan manager."""
    global egressor_client, claude_client

    logger.info("starting_intelligence_service")
    egressor_client = FlowScopeClient()
    claude_client = ClaudeClient()

    yield

    logger.info("shutting_down_intelligence_service")
    if egressor_client:
        await egressor_client.close()


app = FastAPI(
    title="FlowScope Intelligence",
    description="Claude-powered analysis and reasoning for FlowScope",
    version="0.1.0",
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


@app.get("/health")
async def health() -> dict[str, str]:
    """Health check endpoint."""
    return {"status": "ok"}


@app.get("/ready")
async def ready() -> dict[str, str]:
    """Readiness check endpoint."""
    if not settings.anthropic_api_key:
        raise HTTPException(503, "Claude API key not configured")
    return {"status": "ready"}


@app.post("/analyze", response_model=AnalysisResult)
async def analyze(time_range_hours: int = 24) -> AnalysisResult:
    """Perform general analysis of current FlowScope state."""
    if not egressor_client or not claude_client:
        raise HTTPException(503, "Service not initialized")

    try:
        context = await egressor_client.build_analysis_context(time_range_hours)
        result = await claude_client.analyze(context)
        return result
    except ValueError as e:
        raise HTTPException(400, str(e))
    except Exception as e:
        logger.exception("analysis_failed", error=str(e))
        raise HTTPException(500, "Analysis failed")


@app.post("/investigate", response_model=AnalysisResult)
async def investigate(request: InvestigationRequest) -> AnalysisResult:
    """Investigate a specific anomaly."""
    if not egressor_client or not claude_client:
        raise HTTPException(503, "Service not initialized")

    try:
        # Fetch anomaly details
        anomaly = await egressor_client.get_anomaly(request.anomaly_id)
        if not anomaly:
            raise HTTPException(404, "Anomaly not found")

        # Fetch related context
        related_flows = []
        baseline = None

        if request.include_related_flows:
            related_flows = await egressor_client.get_related_flows(anomaly.source_service)

        if request.include_baseline:
            flow_key = f"{anomaly.source_service}|{anomaly.destination_service or 'external'}"
            baseline = await egressor_client.get_baseline(flow_key)

        result = await claude_client.investigate_anomaly(anomaly, related_flows, baseline)
        return result
    except HTTPException:
        raise
    except Exception as e:
        logger.exception("investigation_failed", error=str(e))
        raise HTTPException(500, "Investigation failed")


@app.post("/explain-cost", response_model=AnalysisResult)
async def explain_cost(request: CostExplainRequest) -> AnalysisResult:
    """Explain cost changes between periods."""
    if not egressor_client or not claude_client:
        raise HTTPException(503, "Service not initialized")

    try:
        # Get current cost summary
        cost_summary = await egressor_client.get_cost_summary()
        if not cost_summary:
            raise HTTPException(500, "Failed to fetch cost data")

        # For now, use mock previous data (in production, would query historical data)
        current_cost = cost_summary.total_cost_usd
        previous_cost = current_cost * 0.8  # Mock: assume 20% increase

        top_changes = [
            {
                "service": service,
                "current_cost": cost,
                "previous_cost": cost * 0.75,
                "delta": cost * 0.25,
            }
            for service, cost in list(cost_summary.by_service.items())[:5]
        ]

        result = await claude_client.explain_cost_change(current_cost, previous_cost, top_changes)
        return result
    except HTTPException:
        raise
    except Exception as e:
        logger.exception("cost_explanation_failed", error=str(e))
        raise HTTPException(500, "Cost explanation failed")


@app.post("/ask")
async def ask_question(request: QuestionRequest) -> dict[str, str]:
    """Answer a natural language question."""
    if not egressor_client or not claude_client:
        raise HTTPException(503, "Service not initialized")

    try:
        context = await egressor_client.build_analysis_context(request.context_hours)
        answer = await claude_client.answer_question(request.question, context)
        return {"answer": answer}
    except ValueError as e:
        raise HTTPException(400, str(e))
    except Exception as e:
        logger.exception("question_failed", error=str(e))
        raise HTTPException(500, "Failed to answer question")


@app.get("/optimizations", response_model=list[Optimization])
async def get_optimizations() -> list[Optimization]:
    """Get cost optimization suggestions."""
    if not egressor_client or not claude_client:
        raise HTTPException(503, "Service not initialized")

    try:
        cost_summary = await egressor_client.get_cost_summary()
        top_flows = await egressor_client.get_top_flows(20)

        if not cost_summary:
            return []

        cost_dict = cost_summary.model_dump()
        flows_dict = [f.model_dump() for f in top_flows]

        optimizations = await claude_client.suggest_optimizations(cost_dict, flows_dict)
        return optimizations
    except Exception as e:
        logger.exception("optimization_failed", error=str(e))
        raise HTTPException(500, "Failed to generate optimizations")
