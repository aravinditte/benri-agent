# UNEXECUTED on the build machine (no Docker). Node 24 LTS + tsx runtime.
FROM node:24-slim
WORKDIR /repo
COPY package.json package-lock.json pnpm-workspace.yaml ./
COPY packages ./packages
COPY apps ./apps
COPY fixtures ./fixtures
COPY evals ./evals
COPY scripts ./scripts
RUN npm install --no-audit --no-fund
RUN mkdir -p /data && npx tsx scripts/seed.ts || true
ENV BENRI_DB_PATH=/data/benri.sqlite
CMD ["npx", "tsx", "apps/executor-worker/src/main.ts"]
