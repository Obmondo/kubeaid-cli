#!/bin/bash

set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
DEST="${REPO_ROOT}/pkg/config/parser/k8s-eol.json"
URL="https://endoflife.date/api/kubernetes.json"

TMP="$(mktemp)"
INDENTED="$(mktemp)"

if curl -fsSL --max-time 15 "${URL}" -o "${TMP}"; then
    if jq '.' < "${TMP}" > "${INDENTED}"; then
        mv "${INDENTED}" "${DEST}"
        echo "fetch-k8s-eol: refreshed ${DEST}"
    else
        echo "fetch-k8s-eol: response is not valid JSON, keeping existing ${DEST}" >&2
    fi
else
    echo "fetch-k8s-eol: curl failed, keeping existing ${DEST}" >&2
fi
