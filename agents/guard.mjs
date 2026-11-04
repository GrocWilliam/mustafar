#!/usr/bin/env node
// PreToolUse hook: refuses, during the run, the edits the orchestrator would reject after it.
//
// Rejecting at once saves the rest of an attempt spent on a change that cannot be merged. The orchestrator's checks
// after the run (pipeline.go) stay the authority: Bash can still write anywhere, this only catches the usual ways.
// Exit 2 blocks the tool call and shows stderr to Claude; exit 0 lets it run.
// Node, not Python: Claude Code runs on it, so the image always has it. A missing interpreter would exit 127,
// which Claude Code treats as "allow": the guard would be off without a word.
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const ACCEPTANCE = 'tests/acceptance/';
const WRITE_TOOLS = new Set(['Write', 'Edit', 'MultiEdit', 'NotebookEdit']);
// Git commands no role may run: the orchestrator pushes, and owns branches and history.
const GIT_FORBIDDEN = /\bgit\s+(?:-\S+\s+)*(push|fetch|remote|rebase|switch|filter-branch|filter-repo)\b|\bgit\s+commit\b.*--amend/;
// ponytail: shell heuristic for writes into a path (cp and redirections from elsewhere slip through), the post-run check catches the rest.
// No cp or git checkout/restore: copying a test out, or restoring one as committed, is reading.
const SHELL_WRITE = String.raw`(?:>>?\s*['"]?|\b(?:rm|mv|tee|truncate|touch|chmod)\b[^|;&]*|\bsed\s+-i[^|;&]*|\bperl\s+-\S*i[^|;&]*|\bgit\s+(?:rm|mv)\b[^|;&]*)`;

const escape = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Path prefixes, relative to the repository root, the role must not write. */
function protectedPaths(role, env) {
  const prefixes = (env.PROTECTED_PATHS ?? '.gitea/').split(',').map((p) => p.trim()).filter(Boolean);
  if (role === 'dev') prefixes.push(ACCEPTANCE);
  return prefixes;
}

function frozenReason(rel, prefix) {
  if (prefix === ACCEPTANCE) return `${rel} is a frozen acceptance test: only the tester may change it. Make the code pass it instead.`;
  return `${rel} is a protected path: no agent may change it.`;
}

/** Returns why the tool call is refused, or null. */
export function check(event, role, env = process.env) {
  const tool = event.tool_name ?? '';
  const args = event.tool_input ?? {};
  // POSIX paths: the agent runs on Linux, and the self-check must give the same answers on any host.
  const root = path.posix.resolve(event.cwd || process.cwd());
  if (WRITE_TOOLS.has(tool)) {
    const paths = [args.file_path, args.notebook_path, ...(args.edits ?? []).map((e) => e?.file_path)].filter(Boolean);
    for (const p of paths) {
      const rel = path.posix.relative(root, path.posix.resolve(root, String(p)));
      if (rel.startsWith('../') || path.posix.isAbsolute(rel)) continue; // scratch files outside the repository are fine
      if (role === 'planner' || role === 'reviewer') return `the ${role} only reads: do not modify ${rel}.`;
      if (role === 'tester' && !rel.startsWith(ACCEPTANCE)) return `the tester writes only under ${ACCEPTANCE}: ${rel} is outside it.`;
      for (const prefix of protectedPaths(role, env)) {
        if (rel.startsWith(prefix)) return frozenReason(rel, prefix);
      }
    }
  } else if (tool === 'Bash') {
    const command = args.command ?? '';
    if (GIT_FORBIDDEN.test(command)) return 'do not push, fetch, switch branches, rewrite history or touch remotes: the forge does that.';
    for (const prefix of protectedPaths(role, env)) {
      if (new RegExp(SHELL_WRITE + String.raw`\b` + escape(prefix.replace(/\/$/, ''))).test(command)) return frozenReason(prefix, prefix);
    }
  }
  return null;
}

if (import.meta.url === pathToFileURL(process.argv[1] ?? '').href) {
  const reason = check(JSON.parse(readFileSync(0, 'utf8')), process.env.ROLE ?? '');
  if (reason) {
    process.stderr.write(`Blocked by the forge: ${reason}\n`);
    process.exit(2);
  }
}
