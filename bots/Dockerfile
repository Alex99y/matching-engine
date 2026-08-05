# Build context must be the workspace root:
#   docker build -f bots/Dockerfile -t matching-engine/bots .

FROM node:22-alpine AS builder
WORKDIR /workspace

# bots consumes ts-sdk as a `file:../ts-sdk` dependency resolved through its
# compiled build/ output (npm symlinks it rather than copying — see the
# runtime stage below), so it has to be built before `npm ci` in bots/.
COPY ts-sdk/package.json ts-sdk/package-lock.json ts-sdk/tsconfig.json ts-sdk/
COPY ts-sdk/src ts-sdk/src
RUN npm --prefix ts-sdk ci && npm --prefix ts-sdk run build

COPY bots/package.json bots/package-lock.json bots/tsconfig.json bots/
COPY bots/src bots/src
RUN npm --prefix bots ci && npm --prefix bots run build
RUN npm --prefix bots prune --omit=dev


FROM node:22-alpine
WORKDIR /workspace

# Preserve the same relative layout as the builder: bots/node_modules/ts-sdk
# is a symlink to ../../ts-sdk, so ts-sdk's build output must live at that
# same path here too, or the symlink dangles. ts-sdk has no runtime
# dependencies of its own, so its node_modules isn't needed.
COPY --from=builder /workspace/ts-sdk/build ts-sdk/build
COPY --from=builder /workspace/ts-sdk/package.json ts-sdk/package.json

COPY --from=builder /workspace/bots/dist bots/dist
COPY --from=builder /workspace/bots/node_modules bots/node_modules
COPY --from=builder /workspace/bots/package.json bots/package.json

WORKDIR /workspace/bots
USER node
ENTRYPOINT ["node", "dist/index.js"]
