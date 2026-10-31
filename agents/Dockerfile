# Same image as the CI jobs (runner/config.yaml): the agent tests with exactly the toolchain CI has.
# It already ships git, ssh, make and python3; change both places together.
FROM node:20-bookworm
RUN npm install -g @anthropic-ai/claude-code
COPY --chmod=755 agent.sh /usr/local/bin/agent.sh
COPY prompts /prompts
# Claude Code refuses bypassPermissions as root.
USER node
WORKDIR /home/node
ENTRYPOINT ["agent.sh"]
