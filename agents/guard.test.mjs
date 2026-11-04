// Self-check for guard.mjs: node agents/guard.test.mjs
import assert from 'node:assert/strict';
import { check } from './guard.mjs';

const env = { PROTECTED_PATHS: '.gitea/,Makefile.lock' };
const blocked = (role, tool, args) => check({ tool_name: tool, tool_input: args, cwd: '/w' }, role, env) !== null;
const yes = (role, tool, args) => assert.ok(blocked(role, tool, args), `${role} ${tool} ${JSON.stringify(args)} should be blocked`);
const no = (role, tool, args) => assert.ok(!blocked(role, tool, args), `${role} ${tool} ${JSON.stringify(args)} should run`);

// dev: frozen tests and protected paths, by any edit tool, relative or absolute
yes('dev', 'Edit', { file_path: '/w/tests/acceptance/us-3/run.sh' });
yes('dev', 'Write', { file_path: 'tests/acceptance/us-3/new.test.js' });
yes('dev', 'Write', { file_path: '/w/.gitea/workflows/ci.yml' });
yes('dev', 'Write', { file_path: '/w/src/../tests/acceptance/x' });
yes('dev', 'MultiEdit', { file_path: '/w/src/a.ts', edits: [{ file_path: '/w/tests/acceptance/us-1/t.js' }] });
no('dev', 'Write', { file_path: '/w/src/app.ts' });
no('dev', 'Write', { file_path: '/tmp/scratch.txt' });
no('dev', 'Write', { file_path: '/w/tests/unit/app.test.ts' });

// dev, Bash: writes into frozen tests, not reads or runs
yes('dev', 'Bash', { command: "sed -i 's/3/4/' tests/acceptance/us-3/test.js" });
yes('dev', 'Bash', { command: 'rm -rf tests/acceptance/us-3' });
yes('dev', 'Bash', { command: 'echo skip > tests/acceptance/us-3/run.sh' });
no('dev', 'Bash', { command: 'bash tests/acceptance/us-3/run.sh > /tmp/out 2>&1' });
no('dev', 'Bash', { command: 'cat tests/acceptance/us-3/run.sh | tee /tmp/copy' });
no('dev', 'Bash', { command: 'rm -rf node_modules && bash tests/acceptance/us-3/run.sh' });
no('dev', 'Bash', { command: 'git checkout -- tests/acceptance/us-3/run.sh' });

// every role: no pushing, fetching, rewriting
for (const role of ['dev', 'tester', 'reviewer']) {
  yes(role, 'Bash', { command: 'git push origin HEAD' });
  yes(role, 'Bash', { command: 'git fetch' });
  yes(role, 'Bash', { command: 'git commit --amend -m x' });
  yes(role, 'Bash', { command: 'git rebase -i main' });
}
no('dev', 'Bash', { command: 'git merge origin/main' });
no('dev', 'Bash', { command: "git commit -m 'feat: x'" });

// tester: only under tests/acceptance/
no('tester', 'Write', { file_path: '/w/tests/acceptance/us-3/run.sh' });
yes('tester', 'Edit', { file_path: '/w/src/app.ts' });

// planner and reviewer: read only in the repository
yes('reviewer', 'Edit', { file_path: '/w/src/app.ts' });
yes('planner', 'Write', { file_path: '/w/notes.md' });
no('reviewer', 'Write', { file_path: '/tmp/notes.md' });
no('reviewer', 'Bash', { command: 'git diff origin/main...HEAD' });

console.log('guard ok');
