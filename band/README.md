# Braid band

A Claude Code plugin that draws one line above the prompt: this session cost, today's totals, and the Braid sync state (green, yellow, red dot). It adds no skill, command, agent or hook and sends nothing to the model, so it costs no tokens. It only runs `braid stats --json`.

Install (needs the `braid` CLI on your PATH and a logged-in device):

```
/plugin marketplace add usebraid/braid
/plugin install braid@braid
```

The plugin itself is `plugin/` (what `/plugin install` copies: manifest, hooks module, types, README); everything else here is development tooling and stays out of users' installs.

Development: `cd band && nvm use && npm ci --ignore-scripts && npm run check`.
