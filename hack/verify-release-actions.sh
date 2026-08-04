#!/usr/bin/env bash

set -euo pipefail

workflow=.github/workflows/release.yml
failed=0
while IFS= read -r reference; do
  version="${reference##*@}"
  if [[ ! "${version}" =~ ^[0-9a-f]{40}$ ]]; then
    echo "${workflow}: mutable action reference ${reference}" >&2
    failed=1
  fi
done < <(sed -nE 's/^[[:space:]]*-?[[:space:]]*uses:[[:space:]]*([^@[:space:]]+)@([^[:space:]#]+).*/\1@\2/p' "${workflow}")

if rg -n '^[[:space:]]*-?[[:space:]]*uses:[^#]+@[0-9a-f]{40}[[:space:]]*$' "${workflow}"; then
  echo "${workflow}: every action pin must retain its upstream version comment" >&2
  failed=1
fi

exit "${failed}"
