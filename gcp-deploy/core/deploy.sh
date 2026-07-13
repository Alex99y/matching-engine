#!/bin/bash
set -e

echo "Retrieving secrets from GCP..."
RABBIT_PASS=$(gcloud secrets versions access latest --secret="RABBITMQ_DEFAULT_PASS")
DB_URL=$(gcloud secrets versions access latest --secret="POSTGRESQL_URL")

# Generate the .env file dynamically for Compose
echo "Generating environment configuration..."
cat << EOF > .env
POSTGRESQL_URL=${DB_URL}
RABBITMQ_URL=amqp://admin:${RABBIT_PASS}@10.128.0.2:5672/
EOF

echo "Recreating container with Docker Compose..."
sudo docker compose pull
sudo docker compose up -d --remove-orphans

# Clean up the .env file to avoid leaving secrets on disk
rm .env
echo "Deployment completed successfully."