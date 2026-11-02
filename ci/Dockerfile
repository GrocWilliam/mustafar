# Image of the CI jobs (runner/config.yaml) and base of the agent image (agents/Dockerfile): agents test with exactly
# the toolchain CI has. node:20-bookworm already ships git, ssh, make and python3.
FROM node:20-bookworm
# Debian's Chromium, for browser tests (e.g. playwright-core with executablePath: process.env.CHROMIUM_PATH):
# installed here because agents run without root and cannot add its system libraries themselves.
RUN apt-get update && apt-get install -y --no-install-recommends chromium fonts-dejavu-core \
 && rm -rf /var/lib/apt/lists/*
ENV CHROMIUM_PATH=/usr/bin/chromium
