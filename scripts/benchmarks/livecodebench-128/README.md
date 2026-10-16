# Reproduce the published LiveCodeBench snapshot

From the repository root, run:

```sh
python3 scripts/benchmarks/livecodebench-128/reproduce.py
```

Python 3.9+ is sufficient. No credentials, network, Docker, or third-party packages are needed. The tracked `snapshot.json` contains the selected snapshot's per-task test outcomes, model choices, token usage, answer code hashes, and frozen prices. The script verifies its checksum and recomputes each strategy's pass count and selected-response generation cost. It does not execute candidate code or call providers. Expected counts are Max 88, Flash 85, Jev 91, and Semantic Router 85.

This compact evidence export intentionally excludes task text, private tests, generated source, and credentials. Its recorded grades are evidence inputs, not an independently executable test suite. `code_sha256` ties answers to the full local archive, and `source_manifest_sha256` identifies that archive's integrity manifest.

`archive-reproduction/` preserves the existing full analysis reproduction script, source manifest pins, original generation and Docker scoring scripts, and their instructions. Supply `--data-root` explicitly when running that analysis because the large local archives are not committed. It verifies those archives and reproduces their complete analysis in a new output directory. Original live scripts contain historical absolute paths and require deliberate relocation plus separately provisioned credentials; they are provenance snapshots, not a portable turnkey provider runner. Consult the included README before using them. Never execute generated candidate code on the host.

The report is `docs/benchmarks/livecodebench-128.md`. Neither offline reproduction path promises identical outputs from new provider calls. Prices are historical API-equivalent list prices, not subscription charges. Routing overhead is excluded.
