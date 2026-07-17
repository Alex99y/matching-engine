# GCP Deploy

Scripts and Compose files used to deploy `api`, `core`, and `rabbitmq` to GCP Compute Engine VMs, pulling images from a private Artifact Registry repository.

- `configure.sh` - one-time Docker/Artifact Registry auth setup for a VM
- `api/`, `core/`, `rabbitmq/` - per-service `docker-compose.yml` + `deploy.sh` (pulls secrets from Secret Manager, regenerates `.env`, runs `docker compose up -d`)

## Prerequisites

- [gcloud CLI](https://cloud.google.com/sdk/docs/install) installed
- A GCP project with billing enabled
- Artifact Registry API enabled:

```sh
gcloud services enable artifactregistry.googleapis.com
```

## 1. Configure gcloud locally

Authenticate and point gcloud at the right project:

```sh
gcloud auth login
gcloud config set project <PROJECT_ID>
```

If you'll build/push from your own machine (not just the VM), also configure Docker to use gcloud as a credential helper for the registry region:

```sh
gcloud auth configure-docker us-central1-docker.pkg.dev
```

This is the same command `configure.sh` runs, intended to be executed once on any VM/host that needs to `docker push`/`docker pull` against the registry.

## 2. Create the Artifact Registry repository (one-time)

The Compose files expect a Docker repo named `me-docker-registry` in `us-central1`:

```sh
gcloud artifacts repositories create me-docker-registry \
  --repository-format=docker \
  --location=us-central1 \
  --description="Matching engine service images"
```

## 3. Build the images

Build from the workspace root (build context must be the repo root, per each `Dockerfile`'s header comment):

```sh
go work vendor   # only needed once, or after dependency changes

docker build -f api/Dockerfile  -t us-central1-docker.pkg.dev/<PROJECT_ID>/me-docker-registry/api:latest  .
docker build -f core/Dockerfile -t us-central1-docker.pkg.dev/<PROJECT_ID>/me-docker-registry/core:latest .
```

> Tag must match the `image:` field the target service's `docker-compose.yml` expects, so `docker compose pull` on the VM resolves it. Check [api/docker-compose.yml](api/docker-compose.yml) and [core/docker-compose.yml](core/docker-compose.yml) for the exact `<PROJECT_ID>` currently referenced — the two files use different project ID strings (`e5d919ff-a78f-43b8-856` vs `project-e5d919ff-a78f-43b8-856`), so confirm which one matches your actual GCP project before pushing.

## 4. Push the images

```sh
docker push us-central1-docker.pkg.dev/<PROJECT_ID>/me-docker-registry/api:latest
docker push us-central1-docker.pkg.dev/<PROJECT_ID>/me-docker-registry/core:latest
```

## 5. Deploy on the VM

Each service directory has a `deploy.sh` that:
1. Pulls `RABBITMQ_DEFAULT_PASS`, `JWT_SECRET` (api only), and `POSTGRESQL_URL` (api/core only) from Secret Manager
2. Writes a temporary `.env` for Compose
3. Runs `docker compose pull && docker compose up -d --remove-orphans`
4. Deletes the `.env` file

Run from the VM, inside the relevant service folder:

```sh
cd gcp-deploy/api && ./deploy.sh
cd gcp-deploy/core && ./deploy.sh
cd gcp-deploy/rabbitmq && ./deploy.sh
```

The VM's service account needs `roles/secretmanager.secretAccessor` on those secrets and `roles/artifactregistry.reader` on the repo to pull images.
