# Built on the CI jobs' image (ci/Dockerfile): the agent tests with exactly the toolchain CI has.
FROM ci
RUN npm install -g @anthropic-ai/claude-code
COPY --chmod=755 agent.sh /usr/local/bin/agent.sh
COPY prompts /prompts
# Claude Code refuses bypassPermissions as root.
USER node
WORKDIR /home/node
ENTRYPOINT ["agent.sh"]
