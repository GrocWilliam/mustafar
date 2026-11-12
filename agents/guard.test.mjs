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

// dev and integrator: the support layer is not frozen
no('dev', 'Edit', { file_path: '/w/tests/acceptance/support/sheet.mjs' });
no('integrator', 'Write', { file_path: 'tests/acceptance/support/api.mjs' });
no('dev', 'Bash', { command: "sed -i 's/gold/coins/' tests/acceptance/support/sheet.mjs" });
yes('dev', 'Bash', { command: "sed -i 's/3/4/' tests/acceptance/support/../us-3/test.js" });
// dev, Bash: writes into frozen tests, not reads or runs
yes('dev', 'Bash', { command: "sed -i 's/3/4/' tests/acceptance/us-3/test.js" });
yes('dev', 'Bash', { command: 'rm -rf tests/acceptance/us-3' });
yes('dev', 'Bash', { command: 'echo skip > tests/acceptance/us-3/run.sh' });
no('dev', 'Bash', { command: 'bash tests/acceptance/us-3/run.sh > /tmp/out 2>&1' });
no('dev', 'Bash', { command: 'cat tests/acceptance/us-3/run.sh | tee /tmp/copy' });
no('dev', 'Bash', { command: 'rm -rf node_modules && bash tests/acceptance/us-3/run.sh' });
no('dev', 'Bash', { command: 'git checkout -- tests/acceptance/us-3/run.sh' });

// integrator: fixes code, under the dev's rules
yes('integrator', 'Edit', { file_path: '/w/tests/acceptance/us-3/run.sh' });
yes('integrator', 'Write', { file_path: '/w/.gitea/workflows/ci.yml' });
no('integrator', 'Edit', { file_path: '/w/src/app.ts' });
no('integrator', 'Bash', { command: 'git merge origin/main' });
no('integrator', 'Bash', { command: 'git checkout --theirs tests/acceptance/us-3/run.sh' });

// every role: no pushing, fetching, rewriting
for (const role of ['dev', 'tester', 'retester', 'reviewer', 'integrator']) {
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
no('retester', 'Edit', { file_path: '/w/tests/acceptance/us-50/inventory.test.mjs' });
yes('retester', 'Edit', { file_path: '/w/src/app.ts' });

// planner, architect and reviewer: read only in the repository
yes('reviewer', 'Edit', { file_path: '/w/src/app.ts' });
yes('planner', 'Write', { file_path: '/w/notes.md' });
yes('architect', 'Write', { file_path: '/w/docs/architecture.md' });
no('architect', 'Write', { file_path: '/tmp/design.md' });
no('reviewer', 'Write', { file_path: '/tmp/notes.md' });
no('reviewer', 'Bash', { command: 'git diff origin/main...HEAD' });

// secrets: real credential formats and the agent's own key, in any written content, anywhere
const key = 'sk-ant-oat01-' + 'x'.repeat(40);
const withKey = { ...env, CLAUDE_CODE_OAUTH_TOKEN: key };
const secret = (args, e = env) => check({ tool_name: 'Write', tool_input: args, cwd: '/w' }, 'dev', e) !== null;
assert.ok(secret({ file_path: '/w/src/a.ts', content: `const k = "${key}"` }, withKey));
assert.ok(secret({ file_path: '/tmp/k.txt', content: 'AKIAABCDEFGHIJKLMNOP' }));
assert.ok(secret({ file_path: '/w/a.pem', content: '-----BEGIN OPENSSH PRIVATE KEY-----\nabc' }));
assert.ok(check({ tool_name: 'Edit', tool_input: { file_path: '/w/a.ts', new_string: 'ghp_' + 'a'.repeat(36) }, cwd: '/w' }, 'dev', env));
assert.ok(check({ tool_name: 'MultiEdit', tool_input: { file_path: '/w/a.ts', edits: [{ new_string: 'xoxb-1234567890-abc' }] }, cwd: '/w' }, 'dev', env));
assert.ok(!secret({ file_path: '/w/tests/unit/auth.test.ts', content: "login({ password: 'correct-horse-battery-staple', token: 'abc.def.ghi' })" }));
assert.ok(!secret({ file_path: '/w/README.md', content: 'Set ANTHROPIC_API_KEY in .env' }, withKey));

console.log('guard ok');
