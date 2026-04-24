# LCM: lossless-style context

**Language:** English edition. Full Chinese walkthrough: [LCM_CN.md](./LCM_CN.md).

## What LCM does here

**LCM** (Lossless Context Management) keeps **older conversation content recoverable** under a token budget by combining:

1. **LLM summaries** — chunks of history are summarized; summaries replace raw text in the active context window while originals stay in SQLite.
2. **A DAG** — directed acyclic graph of summary nodes (leaf summaries over messages, condensed summaries over leaves / other condensed nodes) so you can trace provenance and search.

### Why a DAG

- **Nodes:** raw messages, leaf summaries, condensed summaries.
- **Edges:** summaries point **back** to what they summarize (stored via parent/child tables).
- **No cycles:** summaries only reference strictly older material.

### Agent tools

- **`lcm_grep`** — regex search across stored summaries / metadata.
- **`lcm_describe`** — locate time ranges / nodes without pulling full text into the prompt.

Assembly merges **fresh tail** (recent raw messages) with selected summary nodes so the model sees coherent long-range context.

---

## Enabling LCM

In `workspace/config.json` under `agents.defaults.lcm`:

```json
"lcm": {
  "enabled": true,
  "contextThreshold": 0.60,
  "freshTailCount": 16,
  "leafMinFanout": 10,
  "condensedMinFanout": 8,
  "leafChunkTokens": 12000,
  "leafTargetTokens": 600,
  "condensedTargetTokens": 900,
  "incrementalMaxDepth": 1
}
```

Tune thresholds for your model window and cost. **`databasePath`** can override the SQLite file location.

---

## Relationship to token memory

- **`contextWindowTokens`** drives **MemoryConsolidator** (markdown archives).
- **LCM** is a **separate** SQLite-backed subsystem for structured recall.

You can run one, both, or neither; see [CONFIG.md](./CONFIG.md) and [DESIGN.md](./DESIGN.md).

---

## Credits

Implementation ideas are influenced by public **lossless-claw**-style designs; see README **Acknowledgments**.
