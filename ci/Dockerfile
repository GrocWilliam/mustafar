# Image of the CI jobs (runner/config.yaml) and base of the agent image (agents/Dockerfile): agents test with exactly
# the toolchain CI has. node:26-bookworm (the latest Node 26) already ships git, ssh, make and python3.
FROM node:26-bookworm
# Debian's Chromium, for browser tests (e.g. playwright-core with executablePath: process.env.CHROMIUM_PATH):
# installed here because agents run without root and cannot add its system libraries themselves.
RUN apt-get update && apt-get install -y --no-install-recommends chromium fonts-dejavu-core \
 && rm -rf /var/lib/apt/lists/*
ENV CHROMIUM_PATH=/usr/bin/chromium
# playwright-core preinstalled, so acceptance tests load it from here instead of installing it on every run. Read-only use.
RUN mkdir /opt/playwright && cd /opt/playwright && npm init -y >/dev/null && npm i playwright-core@1.64.1 && chmod -R a-w /opt/playwright
ENV PLAYWRIGHT_DIR=/opt/playwright
# The same package, whose chromium.launch() reuses one browser per CI shard: ci.yml points PLAYWRIGHT_DIR here.
COPY playwright-shared.cjs /opt/playwright-shared/node_modules/playwright-core/index.js
RUN echo '{"name":"playwright-core","main":"index.js"}' > /opt/playwright-shared/node_modules/playwright-core/package.json \
 && chmod -R a-w /opt/playwright-shared
