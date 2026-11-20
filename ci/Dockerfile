# Image of the CI jobs (runner/config.yaml) and base of the agent image (agents/Dockerfile): agents test with exactly
# the toolchain CI has. node:26-bookworm (the latest Node 26) already ships git, ssh, make and python3.
FROM node:26-bookworm
# Debian's Chromium, for browser tests (e.g. playwright-core with executablePath: process.env.CHROMIUM_PATH):
# installed here because agents run without root and cannot add its system libraries themselves.
RUN apt-get update && apt-get install -y --no-install-recommends chromium fonts-dejavu-core \
 && rm -rf /var/lib/apt/lists/*
ENV CHROMIUM_PATH=/usr/bin/chromium
# PostgreSQL 16 server binaries (PGDG), for tests against a real database: a test runs `initdb` and `pg_ctl` as the node
# user in a temporary directory. No cluster is created or started at build time.
RUN curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc -o /usr/share/keyrings/pgdg.asc \
 && echo "deb [signed-by=/usr/share/keyrings/pgdg.asc] https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" \
    > /etc/apt/sources.list.d/pgdg.list \
 && mkdir -p /etc/postgresql-common/createcluster.d \
 && echo 'create_main_cluster = false' > /etc/postgresql-common/createcluster.d/no-main.conf \
 && apt-get update && apt-get install -y --no-install-recommends postgresql-16 \
 && rm -rf /var/lib/apt/lists/*
ENV PATH=/usr/lib/postgresql/16/bin:$PATH
# Go, pinned by version and checksum (go.dev/dl).
ARG GO_VERSION=1.27.4
ARG GO_SHA256=7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tgz \
 && echo "${GO_SHA256}  /tmp/go.tgz" | sha256sum -c - \
 && tar -C /usr/local -xzf /tmp/go.tgz && rm /tmp/go.tgz
# GOPATH under the node user's home: agents and CI run as node, which cannot write /usr/local.
ENV PATH=/usr/local/go/bin:/home/node/go/bin:$PATH GOTOOLCHAIN=local
# Bun, pinned.
RUN npm install -g bun@1.4.4
# playwright-core preinstalled, so acceptance tests load it from here instead of installing it on every run. Read-only use.
RUN mkdir /opt/playwright && cd /opt/playwright && npm init -y >/dev/null && npm i playwright-core@1.64.1 && chmod -R a-w /opt/playwright
ENV PLAYWRIGHT_DIR=/opt/playwright
# The same package, whose chromium.launch() reuses one browser per CI shard: ci.yml points PLAYWRIGHT_DIR here.
COPY playwright-shared.cjs /opt/playwright-shared/node_modules/playwright-core/index.js
RUN echo '{"name":"playwright-core","main":"index.js"}' > /opt/playwright-shared/node_modules/playwright-core/package.json \
 && chmod -R a-w /opt/playwright-shared
