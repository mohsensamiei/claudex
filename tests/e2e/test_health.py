"""
E2E Tests for Health Check Endpoints.

Tests the health check and metrics endpoints.
"""

import pytest
import requests


class TestHealthEndpoints:
    """Test health check endpoints."""

    def test_healthz_returns_200(self, base_url: str):
        """Test that /healthz returns 200 OK when Claude CLI is available."""
        response = requests.get(f"{base_url}/healthz", timeout=10)

        assert response.status_code == 200

    def test_metrics_returns_200(self, base_url: str):
        """Test that /metrics returns 200 OK."""
        response = requests.get(f"{base_url}/metrics", timeout=10)

        assert response.status_code == 200

    def test_metrics_contains_prometheus_format(self, base_url: str):
        """Test that /metrics returns Prometheus-compatible format."""
        response = requests.get(f"{base_url}/metrics", timeout=10)

        assert response.status_code == 200

        # Check for Prometheus format indicators
        content = response.text

        # Should contain HELP or TYPE comments (Prometheus format)
        assert "# HELP" in content or "# TYPE" in content

    def test_metrics_contains_request_metrics(self, base_url: str):
        """Test that /metrics contains request-related metrics."""
        response = requests.get(f"{base_url}/metrics", timeout=10)

        assert response.status_code == 200

        content = response.text

        # Should contain our custom metrics
        # At least one of these should be present
        has_metrics = (
            "requests_total" in content
            or "request_duration" in content
            or "active_requests" in content
        )
        assert has_metrics, "Expected request metrics in /metrics output"


class TestEndpointAvailability:
    """Test that all endpoints are available."""

    def test_chat_completions_endpoint_exists(self, base_url: str):
        """Test that /v1/chat/completions endpoint exists."""
        # Send empty body to check endpoint exists
        response = requests.post(
            f"{base_url}/v1/chat/completions",
            json={},
            timeout=10,
        )

        # Should return 400 (bad request) not 404 (not found)
        assert response.status_code != 404

    def test_unknown_endpoint_returns_404(self, base_url: str):
        """Test that unknown endpoints return 404."""
        response = requests.get(f"{base_url}/unknown/endpoint", timeout=10)

        assert response.status_code == 404
