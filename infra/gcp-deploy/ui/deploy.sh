#!/bin/bash
set -e

# No Secret Manager step here, unlike the sibling services: the UI holds no credentials and
# has no runtime configuration at all — the API origin was compiled in by build.sh — so
# there is no .env to generate or delete.

echo "Recreating container with Docker Compose..."
sudo docker compose pull
sudo docker compose up -d --remove-orphans

echo "Deployment completed successfully."
