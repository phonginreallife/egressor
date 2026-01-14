"""Claude API client for FlowScope Intelligence."""

import json
from typing import Any

import anthropic
import structlog

from .config import settings
from .models import AnalysisContext, AnalysisResult, AnomalySummary, Optimization

logger = structlog.get_logger(__name__)

SYSTEM_PROMPT = """You are FlowScope's AI assistant, specialized in analyzing data transfer patterns and costs in Kubernetes environments.

Your role is to:
1. Explain data transfer patterns and anomalies in plain language
2. Identify cost optimization opportunities  
3. Help investigate unexpected traffic or cost increases
4. Provide actionable recommendations

When analyzing data:
- Focus on practical insights that SREs and platform engineers can act on
- Highlight security concerns (unexpected external endpoints, data exfiltration risks)
- Quantify cost impacts when possible
- Be concise but thorough

You receive pre-processed summaries from the FlowScope system - never raw traffic data.
Always structure responses with clear sections for findings, insights, and recommendations."""


class ClaudeClient:
    """Client for Claude API interactions."""

    def __init__(self) -> None:
        self.client = anthropic.Anthropic(api_key=settings.anthropic_api_key)
        self.model = settings.claude_model
        self.max_tokens = settings.claude_max_tokens

    async def analyze(self, context: AnalysisContext) -> AnalysisResult:
        """Perform general analysis of the current state."""
        prompt = self._build_analysis_prompt(context)
        response = await self._query(prompt)
        return self._parse_analysis_response(response)

    async def investigate_anomaly(
        self,
        anomaly: AnomalySummary,
        related_flows: list[dict[str, Any]],
        baseline: dict[str, Any] | None,
    ) -> AnalysisResult:
        """Investigate a specific anomaly."""
        prompt = self._build_investigation_prompt(anomaly, related_flows, baseline)
        response = await self._query(prompt)
        return self._parse_analysis_response(response)

    async def explain_cost_change(
        self,
        current_cost: float,
        previous_cost: float,
        top_changes: list[dict[str, Any]],
    ) -> AnalysisResult:
        """Explain a cost change."""
        delta = current_cost - previous_cost
        percent = (delta / previous_cost * 100) if previous_cost > 0 else 0

        prompt = f"""Explain this data transfer cost change and provide optimization advice:

**Cost Summary:**
- Previous period: ${previous_cost:.2f}
- Current period: ${current_cost:.2f}
- Change: ${delta:+.2f} ({percent:+.1f}%)

**Top Changes by Service:**
{json.dumps(top_changes, indent=2)}

Please explain:
1. Why did costs change?
2. Which services/transfers are responsible?
3. Are these costs justified or wasteful?
4. Specific steps to reduce costs"""

        response = await self._query(prompt)
        return self._parse_analysis_response(response)

    async def answer_question(self, question: str, context: AnalysisContext) -> str:
        """Answer a natural language question."""
        context_json = context.model_dump_json(indent=2)

        prompt = f"""Given this FlowScope data:

{context_json}

Answer this question: {question}

Provide a clear, actionable answer based on the data."""

        return await self._query(prompt)

    async def suggest_optimizations(
        self,
        cost_summary: dict[str, Any],
        top_flows: list[dict[str, Any]],
    ) -> list[Optimization]:
        """Suggest cost optimizations."""
        prompt = f"""Analyze these data transfer patterns and suggest optimizations:

**Cost Summary:**
{json.dumps(cost_summary, indent=2)}

**Top Flows:**
{json.dumps(top_flows, indent=2)}

For each suggestion, provide:
1. Description of the optimization
2. Estimated monthly savings (in USD)
3. Implementation difficulty (low/medium/high)
4. Specific steps to implement
5. Affected services

Format your response as a JSON array of optimizations."""

        response = await self._query(prompt)
        return self._parse_optimizations(response)

    def _build_analysis_prompt(self, context: AnalysisContext) -> str:
        """Build prompt for general analysis."""
        sections = []

        if context.graph_stats:
            sections.append(f"""**Transfer Graph Stats:**
- Total services: {context.graph_stats.total_nodes}
- External endpoints: {context.graph_stats.total_external_nodes}
- Total connections: {context.graph_stats.total_edges}
- Total transfer: {self._format_bytes(context.graph_stats.total_bytes)}
- Egress traffic: {self._format_bytes(context.graph_stats.egress_bytes)}
- Cross-region: {self._format_bytes(context.graph_stats.cross_region_bytes)}""")

        if context.cost_summary:
            sections.append(f"""**Cost Summary (last {context.time_range_hours}h):**
- Total: ${context.cost_summary.total_cost_usd:.2f}
- Egress: ${context.cost_summary.egress_cost_usd:.2f}
- Cross-region: ${context.cost_summary.cross_region_cost_usd:.2f}
- Cross-AZ: ${context.cost_summary.cross_az_cost_usd:.2f}""")

        if context.top_flows:
            flows_text = "\n".join(
                f"- {f.source} → {f.destination}: {self._format_bytes(f.total_bytes)} (${f.cost_usd:.2f})"
                for f in context.top_flows[:10]
            )
            sections.append(f"**Top Flows:**\n{flows_text}")

        if context.active_anomalies:
            anomalies_text = "\n".join(
                f"- [{a.severity.upper()}] {a.type}: {a.source_service} → {a.destination_service or 'external'} "
                f"(+{a.deviation:.1f}σ, ${a.cost_impact_usd:.2f} impact)"
                for a in context.active_anomalies[:5]
            )
            sections.append(f"**Active Anomalies:**\n{anomalies_text}")

        context_text = "\n\n".join(sections)

        return f"""Analyze the following FlowScope data and provide insights:

{context_text}

Please provide:
1. A brief summary of the current state
2. Key findings (3-5 bullet points)
3. Cost insights and optimization opportunities
4. Specific recommendations
5. Risk assessment"""

    def _build_investigation_prompt(
        self,
        anomaly: AnomalySummary,
        related_flows: list[dict[str, Any]],
        baseline: dict[str, Any] | None,
    ) -> str:
        """Build prompt for anomaly investigation."""
        return f"""Investigate this data transfer anomaly:

**Anomaly Details:**
- Type: {anomaly.type}
- Severity: {anomaly.severity}
- Source: {anomaly.source_service}
- Destination: {anomaly.destination_service or 'external'}
- Current value: {self._format_bytes(int(anomaly.current_value))}/hour
- Baseline: {self._format_bytes(int(anomaly.baseline_value))}/hour
- Deviation: {anomaly.deviation:.1f} standard deviations
- Estimated cost impact: ${anomaly.cost_impact_usd:.2f}
- Detected: {anomaly.detected_at.isoformat()}

**Related Flows:**
{json.dumps(related_flows, indent=2)}

**Baseline Statistics:**
{json.dumps(baseline, indent=2) if baseline else "No baseline available"}

Please provide:
1. Root cause analysis - what likely caused this anomaly?
2. Impact assessment - what are the cost and operational implications?
3. Immediate actions - what should be done right now?
4. Long-term recommendations - how to prevent this in the future?"""

    async def _query(self, prompt: str) -> str:
        """Send query to Claude API."""
        if not settings.anthropic_api_key:
            raise ValueError("ANTHROPIC_API_KEY not configured")

        logger.debug("sending_claude_query", prompt_length=len(prompt))

        message = self.client.messages.create(
            model=self.model,
            max_tokens=self.max_tokens,
            system=SYSTEM_PROMPT,
            messages=[{"role": "user", "content": prompt}],
        )

        response_text = message.content[0].text

        logger.info(
            "claude_query_complete",
            input_tokens=message.usage.input_tokens,
            output_tokens=message.usage.output_tokens,
        )

        return response_text

    def _parse_analysis_response(self, response: str) -> AnalysisResult:
        """Parse Claude's response into structured format."""
        result = AnalysisResult(summary="", raw_response=response)

        lines = response.split("\n")
        current_section = ""
        current_content: list[str] = []

        for line in lines:
            line_stripped = line.strip()
            line_lower = line_stripped.lower()

            # Detect section headers
            if any(
                x in line_lower
                for x in ["summary", "overview", "current state"]
            ) and (line_stripped.startswith("#") or line_stripped.endswith(":")):
                self._save_section(result, current_section, current_content)
                current_section = "summary"
                current_content = []
            elif any(x in line_lower for x in ["finding", "key point"]):
                self._save_section(result, current_section, current_content)
                current_section = "findings"
                current_content = []
            elif "cost" in line_lower and "insight" in line_lower:
                self._save_section(result, current_section, current_content)
                current_section = "costs"
                current_content = []
            elif "recommend" in line_lower:
                self._save_section(result, current_section, current_content)
                current_section = "recommendations"
                current_content = []
            elif "risk" in line_lower:
                self._save_section(result, current_section, current_content)
                current_section = "risk"
                current_content = []
            elif line_stripped:
                current_content.append(line_stripped)

        self._save_section(result, current_section, current_content)

        # Fallback: use whole response as summary if nothing parsed
        if not result.summary:
            result.summary = response

        return result

    def _save_section(
        self, result: AnalysisResult, section: str, content: list[str]
    ) -> None:
        """Save parsed content to appropriate result field."""
        if not content:
            return

        if section == "summary":
            result.summary = "\n".join(content)
        elif section == "findings":
            result.key_findings = [
                line.lstrip("-•* ") for line in content if line.startswith(("-", "•", "*", "1", "2", "3", "4", "5"))
            ]
        elif section == "costs":
            result.cost_insights = [
                line.lstrip("-•* ") for line in content if line.startswith(("-", "•", "*", "1", "2", "3", "4", "5"))
            ]
        elif section == "recommendations":
            result.recommendations = [
                line.lstrip("-•* ") for line in content if line.startswith(("-", "•", "*", "1", "2", "3", "4", "5"))
            ]
        elif section == "risk":
            result.risk_assessment = "\n".join(content)

    def _parse_optimizations(self, response: str) -> list[Optimization]:
        """Parse optimization suggestions from response."""
        # Try to extract JSON array from response
        try:
            start = response.find("[")
            end = response.rfind("]") + 1
            if start >= 0 and end > start:
                json_str = response[start:end]
                data = json.loads(json_str)
                return [Optimization(**item) for item in data]
        except (json.JSONDecodeError, TypeError):
            pass

        # Fallback: return single optimization with full text
        return [
            Optimization(
                description=response,
                estimated_monthly_savings_usd=0,
                difficulty="medium",
            )
        ]

    @staticmethod
    def _format_bytes(bytes_val: int) -> str:
        """Format bytes to human-readable string."""
        if bytes_val == 0:
            return "0 B"
        units = ["B", "KB", "MB", "GB", "TB", "PB"]
        i = 0
        val = float(bytes_val)
        while val >= 1024 and i < len(units) - 1:
            val /= 1024
            i += 1
        return f"{val:.1f} {units[i]}"
