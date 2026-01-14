"""Configuration for the Intelligence Service."""

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Application settings loaded from environment variables."""

    model_config = SettingsConfigDict(
        env_prefix="FLOWSCOPE_",
        env_file=".env",
        case_sensitive=False,
    )

    # Service
    host: str = "0.0.0.0"
    port: int = 8090
    debug: bool = False

    # FlowScope API (Go backend)
    api_url: str = "http://localhost:8080"

    # Claude
    anthropic_api_key: str = ""
    claude_model: str = "claude-sonnet-4-20250514"
    claude_max_tokens: int = 4096


settings = Settings()
