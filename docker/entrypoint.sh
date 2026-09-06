#!/usr/bin/env bash
# ==============================================================================
#  NivaroOS Docker Container Process Manager & Entrypoint
# ==============================================================================
set -e

mkdir -p /var/run/nivaroos /var/log/nivaroos /var/lib/nivaroos /etc/nivaroos \
         /DATA/AppData /DATA/Documents /DATA/Downloads /DATA/Media /DATA/Gallery

rm -f /var/run/nivaroos/*.pid /var/run/nivaroos/*.sock /var/run/nivaroos/*.url 2>/dev/null || true

echo "====================================================="
echo "   ✦ NivaroOS Container Platform Starting Up... ✦    "
echo "====================================================="

pids=()

stop_all() {
    echo ""
    echo "Received termination signal. Shutting down NivaroOS microservices gracefully..."
    for pid in "${pids[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
            kill -TERM "$pid" 2>/dev/null || true
        fi
    done
    wait
    echo "All NivaroOS services stopped."
    exit 0
}

trap stop_all SIGTERM SIGINT SIGHUP

start_service() {
    local name="$1"
    local bin="$2"
    shift 2
    echo "➜ Starting ${name} (${bin})..."
    "$bin" "$@" >> "/var/log/nivaroos/${name}.log" 2>&1 &
    local pid=$!
    pids+=("$pid")
}

# 1. Start Message Bus (Event broker)
start_service "message-bus" "/usr/bin/nivaroos-message-bus"
sleep 1

# 2. Start User & Auth Service
start_service "user-service" "/usr/bin/nivaroos-user-service"

# 3. Start Local Storage Manager
start_service "local-storage" "/usr/bin/nivaroos-local-storage"

# 4. Start App Management (Container Studio & Store)
start_service "app-management" "/usr/bin/nivaroos-app-management"

# 5. Start GPU Sidecar (Telemetry)
start_service "gpu-sidecar" "/usr/bin/nivaroos-gpu-sidecar"

# 6. Start Core Daemon (System management & legacy API)
start_service "core" "/usr/bin/nivaroos"

sleep 2

# 7. Start Gateway (Reverse proxy & Web UI server on port 80)
start_service "gateway" "/usr/bin/nivaroos-gateway"

echo "✔ All NivaroOS services started successfully!"
echo "🚀 Web Dashboard is live on http://0.0.0.0:${PORT:-80}"
echo "====================================================="

while true; do
    for pid in "${pids[@]}"; do
        if ! kill -0 "$pid" 2>/dev/null; then
            echo "⚠ NivaroOS service (PID $pid) exited."
        fi
    done
    sleep 5
done
