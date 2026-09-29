
const pr = danger.github.pr;
const body = pr.body || "";
const files = danger.git.modified_files.concat(danger.git.created_files);

if (!/(closes|fixes|resolves)\s+#\d+/i.test(body + " " + pr.title)) {
  fail('Link the issue this PR closes ("Closes #123"). It auto-closes the ticket and lets the delivery scoreboard join this PR to its issue.');
}

if (body.trim().length < 20) {
  warn("This PR has little or no description. A reviewer needs the why and the shape of the change, not just the diff.");
}

const touchedSrc = files.some((f) => /^services\/go-api\/.*\.go$/.test(f) || /^apps\/mobile\/src\/.*\.(ts|tsx)$/.test(f));
const touchedTest = files.some((f) => /_test\.go$/.test(f) || /\.(test|spec)\.(ts|tsx)$/.test(f));
if (touchedSrc && !touchedTest) {
  warn("Source changed but no test file did. If this is a behavior change, it should carry a test that proves it (and goes red first).");
}

const changed = (pr.additions || 0) + (pr.deletions || 0);
if (changed > 600) {
  warn(`This PR changes ${changed} lines. Large diffs are hard to review well and often should be split (bounce to ticketize).`);
}
