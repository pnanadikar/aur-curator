#!/usr/bin/env bash
set -euo pipefail

REMOTE_PRIVATE="origin"
REMOTE_PUBLIC="github"
BRANCH="main"
PUBLIC_REPO_DIR="/tmp/aur-curator-public"

REPO_DIR="$(git rev-parse --show-toplevel)"
cd "${REPO_DIR}"

echo "==> Fetching private main..."
git fetch "${REMOTE_PRIVATE}" "${BRANCH}"

PRIVATE_REF="${REMOTE_PRIVATE}/${BRANCH}"
PRIVATE_COMMIT="$(git rev-parse "${PRIVATE_REF}")"
PRIVATE_TREE="$(git rev-parse "${PRIVATE_REF}^{tree}")"
PRIVATE_SUBJECT="$(git log -1 --format=%s "${PRIVATE_REF}")"

echo "    Source: ${PRIVATE_REF}"
echo "    Commit: ${PRIVATE_COMMIT}"
echo "    Subject: ${PRIVATE_SUBJECT}"

echo "==> Fetching GitHub main..."
git fetch "${REMOTE_PUBLIC}" "${BRANCH}" 2>/dev/null || true

PUBLIC_COMMIT="$(git rev-parse "${REMOTE_PUBLIC}/${BRANCH}" 2>/dev/null || true)"
PUBLIC_TREE="$(git rev-parse "${REMOTE_PUBLIC}/${BRANCH}^{tree}" 2>/dev/null || true)"

if [[ -n "${PUBLIC_COMMIT}" ]]; then
    echo "    GitHub: ${PUBLIC_COMMIT}"
else
    echo "    GitHub: no main branch"
fi

# Idempotency: if the actual file trees are identical, there is nothing to publish.
if [[ -n "${PUBLIC_TREE}" && "${PRIVATE_TREE}" == "${PUBLIC_TREE}" ]]; then
    echo "==> Nothing to publish: Git trees are identical."
    exit 0
fi

echo "==> Creating clean public snapshot..."

rm -rf "${PUBLIC_REPO_DIR}"
mkdir -p "${PUBLIC_REPO_DIR}"

# Export files only -- no .git directory and therefore no private history.
git archive "${PRIVATE_REF}" | tar -x -C "${PUBLIC_REPO_DIR}"

cd "${PUBLIC_REPO_DIR}"

git init -b "${BRANCH}" >/dev/null

# The archive contains only files tracked by the source repository.
# Force-add them because the source .gitignore may exclude tracked files
# such as the built aur-curator binary.
git add -f --all

SHORT_COMMIT="${PRIVATE_COMMIT:0:7}"

git commit \
    -m "Publish private main ${SHORT_COMMIT}" \
    -m "Public snapshot of origin/main." \
    -m "Private commit: ${PRIVATE_COMMIT}" \
    -m "Private subject: ${PRIVATE_SUBJECT}" \
    >/dev/null

PUBLIC_SNAPSHOT="$(git rev-parse HEAD)"

echo "    Snapshot: ${PUBLIC_SNAPSHOT}"

echo "==> Publishing to GitHub..."

GITHUB_URL="$(git -C "${REPO_DIR}" remote get-url "${REMOTE_PUBLIC}")"
git remote add "${REMOTE_PUBLIC}" "${GITHUB_URL}"

git push "${REMOTE_PUBLIC}" "${BRANCH}:${BRANCH}" --force

echo
echo "==> Published successfully."
echo "    Private: ${PRIVATE_COMMIT}"
echo "    Public:  ${PUBLIC_SNAPSHOT}"
