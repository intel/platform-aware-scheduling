#!/bin/sh
echo 'node_health_metric ' "$2" | ssh "$1" -T "cat > /tmp/node-metrics/text.prom"
