# AGENTS.md

Celestia's fork of the Hyperlane Cosmos SDK module (`x/core`, `x/warp`), used by celestia-app. Keep the diff from upstream small so syncs stay easy.

## Pull Requests

- Title: `type(scope)!: [level] subject` (conventional commit), with exactly one of `[high]`, `[medium]`, `[low]`. See [README.md#contributing](README.md#contributing).
- `[high]`: consensus or state-machine-breaking changes, ISM/validator signature verification, mailbox, warp token transfers, security. `[medium]`: other code changes with limited impact. `[low]`: docs, tests, tooling.
- Use the higher level when unsure. Include the tag in the description with a brief reason, e.g. `Review level: [low] — documentation only.`

## Security PRs

- For PRs that resolve Hacken bug bounty reports, do not include details about the bug in the PR description. Instead, link to a Linear issue that contains more details on the bug and the link to the Hacken bug bounty report.
