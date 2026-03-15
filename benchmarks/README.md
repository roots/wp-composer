# WP Composer Benchmarks

Comparative benchmarks: WP Composer vs WPackagist.

## Prerequisites

- `composer` (v2+)
- `curl`
- `jq`
- `hyperfine` (optional, for statistical benchmarking — `brew install hyperfine`)

## Scripts

### `resolve.sh` — Composer resolve times

Measures cold and warm `composer update` times for both repositories using identical plugin sets (small, medium, large).

```bash
./benchmarks/resolve.sh
```

### `metadata.sh` — Repository metadata comparison

Compares metadata download sizes, TTFB, and response times for packages.json, provider files, and p2 endpoints.

```bash
./benchmarks/metadata.sh
```

### `freshness.sh` — Version freshness audit

Checks a sample of popular plugins and compares which versions each repo exposes. Detects gaps or staleness.

```bash
./benchmarks/freshness.sh
```

## Output

Results are written to `benchmarks/results/` (gitignored).
