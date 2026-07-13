#!/bin/bash
set -e

echo "Retrieving secrets from GCP..."
RABBIT_PASS=$(gcloud secrets versions access latest --secret="RABBITMQ_DEFAULT_PASS")

# Generate the .env file dynamically for Compose
echo "Generating environment configuration..."
cat << EOF > .env
RABBITMQ_DEFAULT_PASS=${RABBIT_PASS}
EOF

echo "Recreating container with Docker Compose..."
sudo docker compose pull
sudo docker compose up -d --remove-orphans

# Clean up the .env file to avoid leaving secrets on disk
rm .env
echo "Deployment completed successfully."