#!/usr/bin/env bash
set -euo pipefail

# server-setup.sh — One-time setup for the game server.
# Usage: curl -fsSL https://raw.githubusercontent.com/exploded/game/master/scripts/server-setup.sh | sudo bash

APP_DIR="/var/www/game"
SERVICE="game"
DEPLOY_USER="deploy"

echo "=== game server setup ==="

# Create app directory.
mkdir -p "$APP_DIR"

# The deploy user and its SSH key are shared by every app on this box and set
# up once for the server, not per app. The key is the DEPLOY_SSH_KEY secret.
if ! id "$DEPLOY_USER" &>/dev/null; then
    echo "ERROR: user '$DEPLOY_USER' doesn't exist. Set up the shared deploy user first."
    exit 1
fi

# Create .env file if it doesn't exist.
if [ ! -f "$APP_DIR/.env" ]; then
    cat > "$APP_DIR/.env" <<'ENVEOF'
BASE_URL=https://game.mchugh.au
SES_ACCESS_KEY_ID=
SES_SECRET_ACCESS_KEY=
SES_REGION=ap-southeast-2
MAIL_FROM=StockGame <stockgame@mchugh.au>
NEWS_REPLY_TO=
NEWS_SENDER=
ADMIN_EMAIL=
PORT=8888
PROD=1
TWELVEDATA_API_KEY=
MONITOR_URL=https://monitor.mchugh.au
MONITOR_API_KEY=
ENVEOF
    chmod 600 "$APP_DIR/.env"
    echo "Created $APP_DIR/.env — edit with your credentials."
fi

# Install deploy script.
if [ -f /tmp/game-deploy/deploy-game ]; then
    cp /tmp/game-deploy/deploy-game /usr/local/bin/deploy-game
else
    curl -fsSL https://raw.githubusercontent.com/exploded/game/master/scripts/deploy-game -o /usr/local/bin/deploy-game
fi
chmod +x /usr/local/bin/deploy-game

# Create systemd service.
cat > /etc/systemd/system/$SERVICE.service <<EOF
[Unit]
Description=StockGame Fantasy Market
After=network.target

[Service]
Type=simple
User=www-data
Group=www-data
WorkingDirectory=$APP_DIR
ExecStart=$APP_DIR/game
EnvironmentFile=$APP_DIR/.env
Restart=on-failure
RestartSec=5

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE"

# Sudoers for deploy user.
cat > /etc/sudoers.d/game <<EOF
$DEPLOY_USER ALL=(ALL) NOPASSWD: /usr/local/bin/deploy-game
$DEPLOY_USER ALL=(ALL) NOPASSWD: /usr/bin/systemctl stop $SERVICE
EOF
chmod 440 /etc/sudoers.d/game

chown -R www-data:www-data "$APP_DIR"

# Caddy site block. Caddy (shared by every app on this box) handles TLS, so
# print the block for the Caddyfile rather than editing it here.
echo ""
echo "[note] Add this block to /etc/caddy/Caddyfile, then run: sudo systemctl reload caddy"
cat <<'CADDY'

game.mchugh.au {
	import access_log
	reverse_proxy 127.0.0.1:8888 {
		header_up X-Forwarded-For {client_ip}
		import go_proxy
	}
}

CADDY

echo "=== setup complete ==="
echo ""
echo "Next steps:"
echo "  1. Edit $APP_DIR/.env with your credentials (copy MONITOR_API_KEY from another app's .env)"
echo "  2. Add the Caddy block above, then: sudo systemctl reload caddy"
echo "  3. Make sure the GitHub repo has the DEPLOY_HOST, DEPLOY_USER, DEPLOY_PORT (2222)"
echo "     and DEPLOY_SSH_KEY Actions secrets, then push to master to deploy"
echo "  4. Point the monitor uptime check at https://game.mchugh.au/health"
