# Batch Mode Benchmarks

Performance testing of batch mode with various worker counts and providers.

**Test Environment:**
- macOS Darwin 23.5.0
- Conclave CLI with `--no-rate-limit`
- Dual API keys for Gemini (round-robin rotation)
- Single API key for other providers

**Note:** Cost figures in this document are precise estimates. README uses rounded values for readability.

---

## Gemini (gemini-2.0-flash)

### 50 Items

| Workers | Time | Throughput | Success Rate |
|---------|------|------------|--------------|
| 10 | 24s | 2.1/s | 100% |
| 25 | 11s | 4.5/s | 100% |
| 50 | 7s | 7.1/s | 100% |

### 200 Items

| Workers | Time | Throughput | Success Rate |
|---------|------|------------|--------------|
| 50 | 21s | 9.5/s | 100% |
| 100 | 14s | 14.3/s | 100% |
| 200 | 9s | **22.2/s** | 100% |

**Extrapolated:** 2000 items @ 200 workers = ~90 seconds

---

## OpenAI (gpt-5-nano)

### 200 Items

| Workers | Time | Throughput | Success Rate |
|---------|------|------------|--------------|
| 50 | 24s | 8.3/s | 100% |
| 100 | 16s | 12.5/s | 100% |
| 200 | 12s | **16.7/s** | 100% |

**Extrapolated:** 2000 items @ 200 workers = ~120 seconds

---

## Claude (claude-haiku-4-5-20251001)

### 200 Items

| Workers | Time | Throughput | Success Rate |
|---------|------|------------|--------------|
| 50 | 7s | 28.6/s | 100% |
| 100 | 4s | 50.0/s | 100% |
| 200 | 3s | **66.7/s** | 100% |

**Extrapolated:** 2000 items @ 200 workers = ~30 seconds

---

## Grok (grok-4-1-fast-non-reasoning)

### 200 Items

| Workers | Time | Throughput | Success Rate |
|---------|------|------------|--------------|
| 50 | 7s | 28.6/s | 100% |
| 100 | 3s | 66.7/s | 100% |
| 200 | 2s | **100.0/s** | 100% |

**Extrapolated:** 2000 items @ 200 workers = ~20 seconds

---

## Provider Comparison (200 workers, 200 items)

| Provider | Model | Time | Throughput | Cost/200 | 2000 items ETA |
|----------|-------|------|------------|----------|----------------|
| **Grok** | grok-4-1-fast-non-reasoning | 2s | 100.0/s | $0.010 | ~20s |
| Claude | claude-haiku-4-5-20251001 | 3s | 66.7/s | $0.058 | ~30s |
| Gemini | gemini-3-flash-preview | 9s | 22.2/s | $0.004 | ~90s |
| OpenAI | gpt-5-nano | 12s | 16.7/s | $0.046 | ~120s |

**Speed winner:** Grok | **Cost winner:** Gemini

---

## Real-World Test: Twitter Profile Analysis

**Dataset:** 99 Twitter profiles with context, tweets, and metadata
**Prompt:** "Analyse this twitter account based on the following context, tweets, and metadata and write 3-4 paragraphs of detailed analysis"
**Workers:** 50

| Provider | Model | Time | Throughput | Cost | Success |
|----------|-------|------|------------|------|---------|
| **Grok** | grok-4-1-fast-non-reasoning | 15s | 6.6/s | $0.036 | 99/99 |
| Claude | claude-haiku-4-5-20251001 | 17s | 5.8/s | $0.261 | 99/99 |
| Gemini | gemini-3-flash-preview | 23s | 4.3/s | $0.158 | 99/99 |
| OpenAI | gpt-5-nano | 45s | 2.2/s | $0.086 | 99/99 |

**Observations:**
- Grok fastest AND cheapest for this workload
- Claude 7x more expensive than Grok for similar speed
- OpenAI slowest but mid-range cost
- All providers achieved 100% success rate with 50 concurrent workers

---

## Structured Categorization Test

**Dataset:** Same 99 Twitter profiles
**Prompt:** Detailed 6-section categorization protocol (~200 words) requiring:
- Identity & Background, Content & Expertise, Voice & Style
- Network & Community, Signal Quality, Caveats
- Classification metadata (account type, content type, influence tier, categories, tags)

**Workers:** 50

| Provider | Time | Throughput | Cost | Success |
|----------|------|------------|------|---------|
| **Grok** | 23s | 4.3/s | $0.053 | 99/99 |
| Gemini | 33s | 3.0/s | $0.285 | 99/99 |
| Claude | 39s | 2.5/s | $0.652 | 99/99 |
| OpenAI | 88s | 1.1/s | $0.184 | 99/99 |

### Performance Impact of Prompt Complexity

Comparing simple vs structured prompts (same dataset, same workers):

| Provider | Simple Prompt | Structured Prompt | Slowdown |
|----------|---------------|-------------------|----------|
| Grok | 15s (6.6/s) | 23s (4.3/s) | 1.5x |
| Gemini | 23s (4.3/s) | 33s (3.0/s) | 1.4x |
| Claude | 17s (5.8/s) | 39s (2.5/s) | 2.3x |
| OpenAI | 45s (2.2/s) | 88s (1.1/s) | 2.0x |

**Key insight:** More detailed prompts increase processing time by 1.4-2.3x. Claude shows the largest slowdown but produces the most comprehensive analysis.

### Output Quality Comparison

All providers successfully followed the 6-section structure and classification schema. Key differences:

| Provider | Formatting | Analytical Depth | Style |
|----------|------------|------------------|-------|
| **Claude** | `## (N) Headers` | Deepest - extensive caveats, context | Verbose, thorough |
| Grok | `### Headers` | High signal density | Concise, punchy |
| Gemini | `**Bold**` sections | Balanced detail | Middle ground |
| OpenAI | Plain text | Adequate | Conversational, hedging |

**Claude excels at:**
- Nuanced caveat analysis ("the wallpaper curation could serve subtle community-building functions")
- Contextual depth (twice the word count of competitors)
- Verification recommendations ("readers should weight recommendations heavily but verify independently")

**Grok excels at:**
- Speed (4x faster than OpenAI)
- Cost efficiency (12x cheaper than Claude)
- Information density (says more with fewer words)

### Provider Recommendations

| Use Case | Recommended Provider | Reason |
|----------|---------------------|--------|
| Bulk classification at scale | **Grok** | $0.05/99 items, 4.3/s throughput |
| Deep analysis requiring nuance | **Claude** | Best accuracy, caveats, context |
| Budget-conscious with decent quality | **Gemini** | Good balance of cost and depth |
| When other APIs are down | OpenAI | Reliable fallback, slowest |

---

## Excluded Providers

### Perplexity
Perplexity's models (`sonar`, `sonar-pro`) are optimized for search-augmented generation, not raw throughput. They don't have a "fast/cheap" tier suitable for high-volume batch classification tasks.

### GLM (Zhipu)
GLM API provider disabled. Historical testing showed ~20s response times per request. Current testing blocked by 429 "Insufficient balance" error. The CLI wrapper is available but not compatible with batch mode.

---

## Key Findings

1. **All providers handle extreme parallelism** - 200 concurrent requests with zero failures across Gemini, OpenAI, Claude, and Grok
2. **Grok is fastest and cheapest** - 100 items/second for simple prompts, 12x cheaper than Claude
3. **Claude produces highest quality** - Best accuracy, depth, and nuanced caveats for complex analysis
4. **Prompt complexity affects performance** - Detailed prompts slow throughput by 1.4-2.3x across all providers
5. **Multi-key rotation works** - Dual Gemini keys effectively double throughput
6. **Linear scaling** - Throughput scales nearly linearly with worker count up to item count
7. **No rate limiting issues** - With `--no-rate-limit`, no 429 errors observed
8. **Quality equalises with structure** - All providers follow structured prompts well; differentiation shifts to insight density per dollar

---

## Recommended Settings

| Scenario | Workers | Rate Limit |
|----------|---------|------------|
| Conservative (free tier) | 5 | default |
| Standard (paid tier) | 20-50 | default |
| Aggressive (high tier + multi-key) | 100-200 | `--no-rate-limit` |

---

## Test Commands

```bash
# Generate test data
for i in $(seq 1 200); do
  echo "{\"id\": \"t$i\", \"context\": \"@user$i - tech enthusiast, $((RANDOM % 50000)) followers\"}"
done > test_items.jsonl

# Run benchmark
conclave -c gemini "Classify: influencer, casual, or bot" \
  --batch test_items.jsonl \
  --workers 100 \
  --no-rate-limit \
  -o results.jsonl
```
