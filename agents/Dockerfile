# Built on the CI jobs' image (ci/Dockerfile): the agent tests with exactly the toolchain CI has.
FROM ci
RUN npm install -g @anthropic-ai/claude-code
COPY --chmod=755 agent.sh /usr/local/bin/agent.sh
COPY prompts /prompts
# Forge-wide how-tos (stack-*): personal skills, so they apply in every project. A project's own skills and CLAUDE.md win.
COPY --chown=node:node skills /home/node/.claude/skills
# The guard hook refuses, during the run, the edits the orchestrator rejects after it (frozen tests, protected paths, git pushes).
COPY --chmod=755 guard.mjs /usr/local/bin/forge-guard.mjs
COPY --chown=node:node settings.json /home/node/.claude/settings.json
# Claude Code refuses bypassPermissions as root.
USER node
WORKDIR /home/node
ENTRYPOINT ["agent.sh"]
