# D3 pinned manifests

Each JSON file pins one upstream `vllm-sr` repository revision and records the
size and SHA-256 of runtime artifacts. The nano manifest is the existing pinned
snapshot. For the other five variants, file sizes and Git object identifiers
were read from the Hugging Face model API at the pinned revision. Large-file
SHA-256 values came from LFS metadata; other file bytes were checked against
the Git blob identifier before calculating their SHA-256. Evaluation outputs
and `.gitattributes` are excluded because runtime does not load them.

The manifests include model Python code because the D3 runtime loads upstream
model code. A version update must regenerate and independently review every
file and digest, including executable code, before publication. The snapshot
verifier rejects unlisted Python files and changed or missing artifacts.
