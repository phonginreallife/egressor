#!/bin/bash
# Real-time mock data generator for Egressor
# Usage: ./scripts/mock-realtime.sh [events_per_second] [anomaly_interval_seconds]
#
# Examples:
#   ./scripts/mock-realtime.sh           # Default: 50 events/sec, anomaly every 30s
#   ./scripts/mock-realtime.sh 100       # 100 events/sec
#   ./scripts/mock-realtime.sh 100 60    # 100 events/sec, anomaly every 60s

set -e

API_URL="${API_URL:-http://localhost:8080}"
EVENTS_PER_TICK="${1:-50}"
ANOMALY_INTERVAL="${2:-30}"

echo "🚀 Egressor Real-time Mock Data Generator"
echo "   API URL: $API_URL"
echo "   Events per second: $EVENTS_PER_TICK"
echo "   Anomaly interval: ${ANOMALY_INTERVAL}s"
echo ""
echo "Press Ctrl+C to stop"
echo ""

# Reset existing data
echo "🗑️  Resetting existing mock data..."
curl -s -X DELETE "$API_URL/api/v1/mock/reset" || true
echo ""

TICK=0
TOTAL_EVENTS=0
TOTAL_BYTES=0
ANOMALY_COUNT=0

while true; do
    TICK=$((TICK + 1))
    
    # Generate events
    RESULT=$(curl -s -X POST "$API_URL/api/v1/mock/generate?count=$EVENTS_PER_TICK")
    GENERATED=$(echo "$RESULT" | jq -r '.generated // 0')
    BYTES=$(echo "$RESULT" | jq -r '.total_bytes // 0')
    EGRESS=$(echo "$RESULT" | jq -r '.egress_bytes // 0')
    
    TOTAL_EVENTS=$((TOTAL_EVENTS + GENERATED))
    TOTAL_BYTES=$((TOTAL_BYTES + BYTES))
    
    # Generate anomaly at interval
    if [ $((TICK % ANOMALY_INTERVAL)) -eq 0 ]; then
        ANOMALY=$(curl -s -X POST "$API_URL/api/v1/mock/anomaly")
        ANOMALY_TYPE=$(echo "$ANOMALY" | jq -r '.type // "unknown"')
        ANOMALY_SEVERITY=$(echo "$ANOMALY" | jq -r '.severity // "unknown"')
        ANOMALY_COUNT=$((ANOMALY_COUNT + 1))
        echo "⚠️  Generated anomaly #$ANOMALY_COUNT: $ANOMALY_TYPE ($ANOMALY_SEVERITY)"
    fi
    
    # Format bytes for display
    if [ $TOTAL_BYTES -gt 1073741824 ]; then
        BYTES_DISPLAY="$((TOTAL_BYTES / 1073741824))GB"
    elif [ $TOTAL_BYTES -gt 1048576 ]; then
        BYTES_DISPLAY="$((TOTAL_BYTES / 1048576))MB"
    elif [ $TOTAL_BYTES -gt 1024 ]; then
        BYTES_DISPLAY="$((TOTAL_BYTES / 1024))KB"
    else
        BYTES_DISPLAY="${TOTAL_BYTES}B"
    fi
    
    # Print status every second
    printf "\r📊 Tick: %d | Events: %d | Data: %s | Anomalies: %d    " \
        "$TICK" "$TOTAL_EVENTS" "$BYTES_DISPLAY" "$ANOMALY_COUNT"
    
    sleep 1
done
