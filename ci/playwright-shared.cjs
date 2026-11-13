// playwright-core as the tests load it (createRequire(PLAYWRIGHT_DIR + '/')('playwright-core')), except that
// chromium.launch() connects to the browser the CI shard started once (PW_WS_ENDPOINT) instead of starting one per
// script; close() then only drops this script's contexts. Launch args other than --no-sandbox get a browser of their own.
const pw = require('/opt/playwright/node_modules/playwright-core');
const launch = pw.chromium.launch.bind(pw.chromium);
pw.chromium.launch = async (options = {}) => {
  const ws = process.env.PW_WS_ENDPOINT;
  const ownArgs = (options.args ?? []).some((a) => a !== '--no-sandbox');
  if (!ws || ownArgs || options.headless === false) return launch(options);
  try {
    return await pw.chromium.connect(ws);
  } catch {
    return launch(options); // the shared browser died: this script still runs, on its own
  }
};
module.exports = pw;
