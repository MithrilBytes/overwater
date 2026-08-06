#!/usr/bin/env bash
# Build embeddings for the docs search index.
set -euo pipefail

curl -s https://api.voyageai.com/v1/embeddings \
  -H "Authorization: Bearer ${VOYAGE_API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "voyage-3.5",
    "input": ["how do i rotate an api key", "billing faq", "sso setup"],
    "input_type": "document"
  }' | jq -r '.data[].embedding' > embeddings.jsonl
