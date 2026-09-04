# GCP Deploy

Scripts and Compose files used to deploy `api`, `core`, `rabbitmq`, and `ui` to GCP Compute Engine VMs, pulling images from a private Artifact Registry repository.

- `configure.sh` - one-time Docker/Artifact Registry auth setup for a VM
- `api/`, `core/`, `rabbitmq/` - per-service `docker-compose.yml` + `deploy.sh` (pulls secrets from Secret Manager, regenerates `.env`, runs `docker compose up -d`)
- `ui/` - static React bundle served by nginx; adds a `build.sh` because this image is built with its API origin compiled in (see [Building the UI image](#building-the-ui-image)) and its `deploy.sh` has no secrets to fetch

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

> Tag must match the `image:` field the target service's `docker-compose.yml` expects, so `docker compose pull` on the VM resolves it. Check [api/docker-compose.yml](api/docker-compose.yml) and [core/docker-compose.yml](core/docker-compose.yml) for the exact `<PROJECT_ID>` currently referenced — the two files use different project ID strings (`e5d919ff-a78f-43b8-856` vs `project-e5d919ff-a78f-43b8-856`), so confirm which one matches your actual GCP project before pushing. [ui/docker-compose.yml](ui/docker-compose.yml) follows the `api` one, since the two are deployed as a pair.

### Building the UI image

The UI does **not** build with a plain `docker build` line like the Go services, because Vite
inlines the API origin into the JavaScript at build time. There is no environment variable to set
on the running container: one image serves exactly one API. [ui/build.sh](ui/build.sh) exists to
make that explicit and refuses to run without an origin:

```sh
VITE_API_URL=https://api.example.com ./ui/build.sh              # build only
VITE_API_URL=https://api.example.com PUSH=true ./ui/build.sh    # build and push
```

`PROJECT_ID`, `REGION`, `REPOSITORY` and `TAG` override the image coordinates; the defaults match
`ui/docker-compose.yml`. Run it from a machine holding the repo (it builds from the workspace
root — the UI compiles `ts-sdk` from source), not from the VM.

> Because the origin is compiled in, re-pointing the deployed UI at a different API means
> rebuilding and re-pushing the image, then re-running `ui/deploy.sh`. The login screen's
> Host/Port fields stay editable at runtime, so a wrong bake is recoverable by hand for a single
> user — but not for everyone hitting the site.

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
cd infra/gcp-deploy/api && ./deploy.sh
cd infra/gcp-deploy/core && ./deploy.sh
cd infra/gcp-deploy/rabbitmq && ./deploy.sh
cd infra/gcp-deploy/ui && ./deploy.sh
```

The VM's service account needs `roles/secretmanager.secretAccessor` on those secrets and `roles/artifactregistry.reader` on the repo to pull images. `ui/deploy.sh` skips steps 1, 2 and 4 — the UI holds no credentials and has no runtime configuration — so it only needs the Artifact Registry role.

The UI publishes on port **80**, which needs a firewall rule the other services don't:

```sh
gcloud compute firewall-rules create allow-ui-http \
  --allow=tcp:80 --target-tags=ui --description="Public UI"
```

Its container answers `GET /healthz` with `ok`, which is what the Compose healthcheck uses and what an external HTTP load balancer or uptime check should point at.
