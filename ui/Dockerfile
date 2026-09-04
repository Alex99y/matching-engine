# Build context must be the workspace root — the UI imports ts-sdk straight from source
# through the alias in ui/vite.config.ts, so both trees have to be inside the context:
#
#   docker build -f ui/Dockerfile \
#     --build-arg VITE_API_URL=https://api.example.com \
#     -t matching-engine/ui .
#
# VITE_API_URL is baked in here and cannot be changed on the running container — see the
# note above the ARG below.

FROM node:22-alpine AS builder
WORKDIR /workspace

# ts-sdk is consumed as TypeScript source (vite compiles it as part of the app) and has no
# runtime dependencies of its own, so it needs no install step — only to sit at the relative
# path the alias resolves.
COPY ts-sdk/src/ ts-sdk/src/

WORKDIR /workspace/ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci

COPY ui/ ./

# Vite inlines this into the bundle at build time; nothing reads it at container start, so
# one image serves exactly one API origin and pointing it elsewhere means rebuilding. An
# empty or missing value falls back to http://localhost:4000 (see ui/src/config.ts), which
# is only ever right for a local build.
ARG VITE_API_URL
ENV VITE_API_URL=${VITE_API_URL}
RUN npm run build


FROM nginx:1.29-alpine
COPY ui/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /workspace/ui/dist /usr/share/nginx/html
EXPOSE 80
