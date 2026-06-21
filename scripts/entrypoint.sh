#!/bin/sh
set -e

CLAUDE_DIR="${HOME}/.claude"
CREDENTIALS_FILE="${CLAUDE_DIR}/.credentials.json"
MCP_FILE="${CLAUDE_DIR}/.mcp.json"

# Ensure .claude directory exists with correct permissions
mkdir -p "${CLAUDE_DIR}"

# Remove or reset MCP config to avoid hanging on unavailable servers
# The MCP servers configured on the host won't be available in the container
# Create empty MCP config if not exists (ensures clean MCP state)
if [ ! -f "${MCP_FILE}" ]; then
    echo "Creating minimal MCP config for container environment"
    echo '{"mcpServers":{}}' > "${MCP_FILE}" 2>/dev/null || true
fi

# Credential bootstrap.
#
# IMPORTANT: the Claude CLI auto-refreshes the access token via the refreshToken
# and writes the new token back to ${CREDENTIALS_FILE}. For that to keep working
# across restarts, ${CLAUDE_DIR} must be a WRITABLE, PERSISTENT volume (a Docker
# bind mount / named volume, or a K8s PVC) — NOT a read-only secret mount or a
# read-only single-file bind mount.
#
# Sources (env var, or a read-only seed path such as a K8s secret) are only used
# to SEED the credentials when none exist yet in the writable volume. We never
# overwrite an existing file, otherwise every restart would clobber the freshly
# refreshed token with the stale bootstrap value and force a re-login. Set
# CLAUDE_CREDENTIALS_FORCE=1 to re-seed from source anyway.

if [ -f "${CREDENTIALS_FILE}" ] && [ "${CLAUDE_CREDENTIALS_FORCE}" != "1" ]; then
    echo "Existing credentials found at ${CREDENTIALS_FILE}; keeping refreshed token"
# Seed from a read-only file path (e.g. a K8s secret mounted at this location).
elif [ -n "${CLAUDE_CREDENTIALS_SEED}" ] && [ -f "${CLAUDE_CREDENTIALS_SEED}" ]; then
    echo "Seeding credentials from ${CLAUDE_CREDENTIALS_SEED}"
    cp "${CLAUDE_CREDENTIALS_SEED}" "${CREDENTIALS_FILE}"
    chmod 600 "${CREDENTIALS_FILE}"
# Seed from the CLAUDE_CODE_OAUTH_TOKEN environment variable.
elif [ -n "${CLAUDE_CODE_OAUTH_TOKEN}" ]; then
    echo "Seeding credentials from CLAUDE_CODE_OAUTH_TOKEN environment variable"

    # Extract components from the token if it's a full JSON
    if echo "${CLAUDE_CODE_OAUTH_TOKEN}" | grep -q "accessToken"; then
        # Full JSON provided
        echo "${CLAUDE_CODE_OAUTH_TOKEN}" > "${CREDENTIALS_FILE}"
    else
        # Just the access token provided, create minimal credentials.
        # NOTE: a bare access token has no refreshToken, so the CLI CANNOT
        # auto-refresh it — provide the full credentials JSON for persistence.
        # Expiry set to 1 year from now (in milliseconds)
        EXPIRY=$(( $(date +%s) * 1000 + 31536000000 ))
        cat > "${CREDENTIALS_FILE}" << EOF
{
  "claudeAiOauth": {
    "accessToken": "${CLAUDE_CODE_OAUTH_TOKEN}",
    "expiresAt": ${EXPIRY},
    "scopes": ["user:inference"],
    "subscriptionType": "max",
    "rateLimitTier": "default_claude_max_20x"
  }
}
EOF
    fi
    chmod 600 "${CREDENTIALS_FILE}"
fi

# Check if credentials exist
if [ -f "${CREDENTIALS_FILE}" ]; then
    # Check token expiration
    EXPIRES_AT=$(cat "${CREDENTIALS_FILE}" | grep -o '"expiresAt":[0-9]*' | grep -o '[0-9]*' || echo "0")
    CURRENT_MS=$(( $(date +%s) * 1000 ))

    if [ "${EXPIRES_AT}" -gt 0 ] && [ "${EXPIRES_AT}" -lt "${CURRENT_MS}" ]; then
        echo "WARNING: OAuth token has expired! Token expired at $(date -d @$((EXPIRES_AT/1000)))"
        echo "Please update CLAUDE_CODE_OAUTH_TOKEN or mount fresh credentials"
    else
        EXPIRES_DATE=$(date -d @$((EXPIRES_AT/1000)) 2>/dev/null || echo "unknown")
        echo "OAuth token valid until: ${EXPIRES_DATE}"
    fi
else
    echo "WARNING: No credentials found at ${CREDENTIALS_FILE}"
    echo "Set CLAUDE_CODE_OAUTH_TOKEN env var or mount credentials file"
fi

# Start the server
exec /app/server "$@"
