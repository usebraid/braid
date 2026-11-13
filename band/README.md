# Braid band

A Claude Code plugin that draws one line above the prompt: this session's cost and tokens, today's totals, and the Braid sync state (green, yellow, red dot). It adds no skill, command, agent or hook and sends nothing to the model, so it costs no tokens. It only runs `braid stats --json`.

Install (needs the `braid` CLI on your PATH and a logged-in device):

```
/plugin marketplace add usebraid/braid
/plugin install braid@braid
```

Development: `cd band && nvm use && npm ci --ignore-scripts && npm run check`.
