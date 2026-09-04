#!/bin/bash
set -e


REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
IMAGE="us-central1-docker.pkg.dev/project-e5d919ff-a78f-43b8-856/me-docker-registry/ui:latest"
VITE_API_URL="34.96.85.4:80"

echo "Building ${IMAGE}"
echo "  API origin baked in: ${VITE_API_URL}"
docker build \
  -f "${REPO_ROOT}/ui/Dockerfile" \
  --build-arg "VITE_API_URL=${VITE_API_URL}" \
  -t "${IMAGE}" \
  "${REPO_ROOT}"

if [ "${PUSH}" = "true" ]; then
  echo "Pushing ${IMAGE}..."
  docker push "${IMAGE}"
  echo "Build and push completed successfully."
else
  echo "Build completed successfully. Not pushed — to publish it run:"
  echo "  docker push ${IMAGE}"
  echo "(or re-run with PUSH=true)"
fi
