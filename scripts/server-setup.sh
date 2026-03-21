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

# Create deploy user if it doesn't exist.
if ! id "$DEPLOY_USER" &>/dev/null; then
    useradd -m -s /bin/bash "$DEPLOY_USER"
    echo "Created user: $DEPLOY_USER"
fi

# Create .env file if it doesn't exist.
if [ ! -f "$APP_DIR/.env" ]; then
    cat > "$APP_DIR/.env" <<'ENVEOF'
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
BASE_URL=https://game.yourdomain.com
ADMIN_EMAIL=
PORT=8888
PROD=1
TWELVEDATA_API_KEY=
ENVEOF
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
WorkingDirectory=$APP_DIR
ExecStart=$APP_DIR/game
EnvironmentFile=$APP_DIR/.env
Restart=on-failure
RestartSec=5

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

chown -R www-data "$APP_DIR"

echo "=== setup complete ==="
echo "Edit $APP_DIR/.env with your credentials, then start with: sudo systemctl start $SERVICE"
