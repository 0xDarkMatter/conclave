# Batch Mode

> Process thousands of items efficiently with parallel workers, rate limiting, and resume capability.

Batch mode is designed for pipeline workloads: classifying datasets, processing documents, or running large-scale analysis. It automatically uses cheap models, handles rate limits, and can resume interrupted jobs.

---

## Quick Start

```bash
# Create a JSONL input file
cat > items.jsonl << 'EOF'
{"id": "1", "context": "@acme_corp - Enterprise software solutions, 50K followers"}
{"id": "2", "context": "@jane_dev - Software engineer and blogger, 2K followers"}
{"id": "3", "context": "@wanderlust99 - Travel photos and adventures, 500 followers"}
EOF

# Run batch classification
conclave --all "Classify as: PUBLIC_FIGURE, BRAND, or PERSONAL" \
  --batch items.jsonl \
  --output results.jsonl

# Results stream to output as they complete
cat results.jsonl
```

---

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--batch FILE` | - | JSONL input file (required for batch mode) |
| `--workers N` | 5 | Number of parallel workers |
| `--output FILE` | stdout | Output file path |
| `--resume` | false | Skip already-processed items |
| `--retries N` | 0 | Retry failed items N times with exponential backoff |
| `--no-rate-limit` | false | Disable rate limiting (for high-tier accounts) |

Batch mode automatically implies:
- `-c` (cheap mode) - uses fast, cost-effective models
- `-g` (API mode) - uses APIs directly, no CLI tools required

---

## Input Format

Each line is a JSON object with at least an `id` field:

```jsonl
{"id": "item_001", "prompt": "Classify this account", "context": "@acme_corp - Enterprise software, 50K followers"}
{"id": "item_002", "context": "@jane_dev - Software engineer and blogger, 2K followers"}
{"id": "item_003", "prompt": "Summarize this", "context": "Long document text here..."}
```

### Fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | Yes | Unique identifier (used for resume/deduplication) |
| `prompt` | No | Per-item prompt (overrides CLI prompt if provided) |
| `context` | No | Per-item context - can be substantial (see below) |
| *other* | No | Extra fields are passed through to output |

**Notes:**
- If `id` is missing, line number is used (with a warning)
- If `prompt` is missing, uses the CLI prompt argument
- Extra fields (e.g., `username`, `category`) are preserved in output

### Context Field Size

The `context` field has **no arbitrary limit** - it passes directly to the LLM APIs. The practical limit is the smallest context window among your providers (~100K tokens / ~400KB text to be safe across all providers).

This means each item can contain substantial content:

```jsonl
{"id": "user_123", "context": "Username: @acme_corp\nBio: Enterprise software solutions for modern teams...\nFollowers: 50K\nCreated: 2015\n\nRecent posts (last 500):\n1. Excited to announce our Series B funding...\n2. New integration with Slack now available...\n3. ...\n\nProfile history:\n...\n\nEngagement metrics:\n..."}
```

**What fits in ~100K tokens:**
- ~400KB of plain text
- ~10,000 tweets with metadata
- A full codebase file
- An entire research paper
- Complete user profile with years of history

---

## Output Format

Results are written as JSONL, one per line:

```jsonl
{"id": "item_001", "verdict": "PUBLIC_FIGURE", "confidence": "high", "reasoning": "CEO position indicates public figure", "cost_usd": 0.002}
{"id": "item_002", "verdict": "PUBLIC_FIGURE", "confidence": "high", "reasoning": "CEO position indicates public figure", "cost_usd": 0.002}
```

### Output Fields

| Field | Description |
|-------|-------------|
| `id` | Item identifier (from input) |
| `verdict` | The synthesized verdict |
| `confidence` | Confidence level (high/medium/low) |
| `reasoning` | Brief explanation |
| `cost_usd` | Estimated cost for this item |
| `duration_ms` | Processing time in milliseconds |
| `error` | Error message (if processing failed) |
| `providers` | Individual provider responses (with `--verbose`) |

Extra fields from input are preserved in output.

---

## Resume & Checkpoints

When using `--output FILE`, a checkpoint file (`FILE.checkpoint`) tracks completed items.

```bash
# Initial run (interrupted after 500 items)
conclave --all "Classify" --batch items.jsonl --output results.jsonl
# ^C

# Resume from where we left off
conclave --all "Classify" --batch items.jsonl --output results.jsonl --resume
# Skips 500 already-processed items, continues with remainder
```

**How it works:**
1. Checkpoint file stores completed IDs (one per line)
2. On resume, already-processed IDs are skipped
3. Results are appended to the existing output file
4. Checkpoint is updated atomically after each item

**Important:** Keep the input file unchanged between runs for resume to work correctly.

---

## Rate Limiting

Batch mode includes automatic rate limiting to avoid 429 errors:

| Provider Count | Interval | Effective RPM |
|----------------|----------|---------------|
| 1 provider | 1s | ~60 |
| 2 providers | 3s | ~20 |
| 3-4 providers | 4s | ~15 |
| 5+ providers | 6s | ~10 |

The rate limiter is adaptive:
- Slows down on 429 errors
- Speeds up after successful streaks

---

## Performance Tuning

### Worker Count

Choose workers based on your use case:

| Scenario | Recommended Workers |
|----------|---------------------|
| Default | 5 |
| Many providers (5+) | 3-5 |
| Fast responses needed | 10 |
| Rate limit concerns | 2-3 |

```bash
# More parallel workers for faster throughput
conclave --all "Classify" --batch items.jsonl --workers 10 --output results.jsonl

# Fewer workers for rate-limited scenarios
conclave --all "Classify" --batch items.jsonl --workers 2 --output results.jsonl
```

### Timeout

Default timeout is 60 seconds per item. Adjust for long-running queries:

```bash
conclave --all "Deep analysis" --batch items.jsonl --timeout 120 --output results.jsonl
```

### High-Tier API Accounts

If you have enterprise or high-tier API limits (e.g., Anthropic Tier 4 = 4,000 RPM), you can disable rate limiting entirely:

```bash
# Single provider, no rate limit, max parallelism
conclave claude "Classify as BRAND/PERSONAL/BOT" \
  --batch items.jsonl \
  --workers 50 \
  --no-rate-limit \
  --output results.jsonl
```

**Expected throughput with `--no-rate-limit`:**
- 50 workers × ~2s response time = ~1,500 items/minute
- 2,000 items in ~90 seconds

**Recommended workflow:**
1. Test with multiple providers on a small sample (~50 items)
2. Review quality, pick the best performer
3. Run full batch with single provider + `--no-rate-limit`

---

## Converting Data

### From CSV (using jq + miller)

```bash
mlr --c2j cat accounts.csv | jq -c '{id: .username, context: .bio}' > items.jsonl
```

### From CSV (using Python)

```python
import csv
import json

with open('accounts.csv') as f:
    reader = csv.DictReader(f)
    for row in reader:
        print(json.dumps({'id': row['id'], 'context': row['bio']}))
```

### From JSON array

```bash
jq -c '.[]' array.json > items.jsonl
```

---

## Cost Estimation

Batch mode uses cheap models by default:

| Provider | Model | Input $/M | Output $/M |
|----------|-------|-----------|------------|
| gemini | gemini-3-flash-preview | $0.50 | $3.00 |
| openai | gpt-5-nano | $0.10 | $0.40 |
| claude | claude-haiku-4-5 | $1.00 | $5.00 |
| perplexity | sonar | $1.00 | $1.00 |
| grok | grok-4-1-fast-non-reasoning | $0.20 | $0.50 |

**Estimated cost per item (5 providers + judge):** ~$0.002-0.005

**For 2000 items:** ~$4-10

---

## Examples

### Basic Classification

```bash
conclave --all "Classify as spam/ham" --batch messages.jsonl --output classified.jsonl
```

### With Specific Providers

```bash
conclave gemini,claude "Analyze sentiment" --batch tweets.jsonl --output sentiment.jsonl
```

### Per-Item Prompts

Input file with per-item prompts:

```jsonl
{"id": "1", "prompt": "Translate to French", "context": "Hello world"}
{"id": "2", "prompt": "Translate to Spanish", "context": "Good morning"}
```

```bash
conclave --all --batch translate.jsonl --output translated.jsonl
```

### Verbose Output

Include full provider responses:

```bash
conclave --all "Analyze" --batch items.jsonl --output results.jsonl --verbose
```

### Pipe Input

```bash
cat items.jsonl | conclave --all "Classify" --batch - > results.jsonl
```

---

## Troubleshooting

### "No prompt provided"

Either:
1. Include `prompt` field in each JSONL line, or
2. Provide a default prompt on the command line

### "Rate limit (429) errors"

Reduce workers or wait for the adaptive rate limiter to adjust:

```bash
conclave --all "Query" --batch items.jsonl --workers 2 --output results.jsonl
```

### "Resume not working"

Check that:
1. Output file path matches the original run
2. Input file hasn't been modified
3. `--resume` flag is set

### "Checkpoint file is stale"

Remove it to start fresh:

```bash
rm results.jsonl.checkpoint
rm results.jsonl
conclave --all "Query" --batch items.jsonl --output results.jsonl
```

---

## Summary Output

After batch completion, a summary is printed to stderr:

```
Batch complete: 2000 items processed (47m23s)
  Success: 1987 (99.4%) | Failed: 13 (0.7%)
  Estimated cost: $4.52
```
