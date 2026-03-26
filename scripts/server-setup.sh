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

# Create SSH key pair for GitHub Actions (reuse if exists).
SSH_DIR="/home/$DEPLOY_USER/.ssh"
SSH_KEY="$SSH_DIR/github_actions"
mkdir -p "$SSH_DIR"
if [ ! -f "$SSH_KEY" ]; then
    ssh-keygen -t ed25519 -f "$SSH_KEY" -N "" -C "github-actions-deploy"
    cat "$SSH_KEY.pub" >> "$SSH_DIR/authorized_keys"
    echo "Created SSH key: $SSH_KEY"
    echo "Add this PRIVATE key as DEPLOY_SSH_KEY in GitHub repo secrets:"
    echo "---"
    cat "$SSH_KEY"
    echo "---"
fi
chown -R "$DEPLOY_USER:$DEPLOY_USER" "$SSH_DIR"
chmod 700 "$SSH_DIR"
chmod 600 "$SSH_KEY" "$SSH_DIR/authorized_keys"

# Create .env file if it doesn't exist.
if [ ! -f "$APP_DIR/.env" ]; then
    cat > "$APP_DIR/.env" <<'ENVEOF'
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
BASE_URL=https://game.mchugh.au
ADMIN_EMAIL=
PORT=8888
PROD=1
TWELVEDATA_API_KEY=
MONITOR_URL=
MONITOR_API_KEY=
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

# Nginx reverse proxy.
DOMAIN="game.mchugh.au"

apt-get install -y certbot python3-certbot-nginx > /dev/null 2>&1 || true

if [ ! -f "/etc/nginx/sites-available/$SERVICE" ]; then
    cat > "/etc/nginx/sites-available/$SERVICE" <<NGINXEOF
server {
    listen 80;
    listen [::]:80;
    server_name $DOMAIN;

    location / {
        proxy_pass http://127.0.0.1:8888;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
NGINXEOF
    ln -sf "/etc/nginx/sites-available/$SERVICE" "/etc/nginx/sites-enabled/$SERVICE"
    nginx -t && systemctl reload nginx
    echo "Nginx config created for $DOMAIN"
else
    echo "Nginx config already exists for $SERVICE"
fi

# SSL via certbot (non-interactive).
if [ ! -d "/etc/letsencrypt/live/$DOMAIN" ]; then
    certbot --nginx -d "$DOMAIN" --non-interactive --agree-tos --redirect -m admin@mchugh.au
    echo "SSL certificate installed for $DOMAIN"
else
    echo "SSL certificate already exists for $DOMAIN"
fi

echo "=== setup complete ==="
echo ""
echo "Next steps:"
echo "  1. Edit $APP_DIR/.env with your credentials"
echo "  2. Add these GitHub repo secrets:"
echo "     DEPLOY_HOST    = <server IP>"
echo "     DEPLOY_USER    = deploy"
echo "     DEPLOY_SSH_KEY = (contents of /home/deploy/.ssh/github_actions)"
echo "     DEPLOY_PORT    = 22"
echo "  3. Start with: sudo systemctl start $SERVICE"
