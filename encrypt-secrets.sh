#!/usr/bin/env bash
# Encrypt plaintext secret files into secrets/*.enc using the public key.
# No secret key needed — only the public key (committed) is used.
set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SECRETS_DIR="${REPO_ROOT}/secrets"
PUBLIC_KEY_FILE="${SECRETS_DIR}/public-key.txt"

if [ ! -f "$PUBLIC_KEY_FILE" ]; then
    echo "Error: Public key not found at ${PUBLIC_KEY_FILE}" >&2
    exit 1
fi

PUBLIC_KEY=$(grep -E '^age1' "$PUBLIC_KEY_FILE" | head -1)
if [ -z "$PUBLIC_KEY" ]; then
    echo "Error: No age public key (age1...) found in ${PUBLIC_KEY_FILE}" >&2
    exit 1
fi

if ! command -v age >/dev/null 2>&1; then
    echo "Error: 'age' is not installed (brew install age)." >&2
    exit 1
fi

encrypt_one() {
    local src_file="$1"
    local enc_name="$2"
    local enc_file="${SECRETS_DIR}/${enc_name}"
    if [ ! -f "$src_file" ]; then
        echo "  (skip: $src_file not found)"
        return 0
    fi
    if age -e -r "$PUBLIC_KEY" -o "$enc_file" "$src_file" 2>/dev/null; then
        echo "  encrypted: $(basename "$src_file") -> secrets/$enc_name"
    else
        echo "  (failed: $src_file)" >&2
        return 1
    fi
}

echo "Encrypting secrets (using public key only)..."
encrypt_one "${REPO_ROOT}/secrets.yaml" "secrets.yaml.enc"
encrypt_one "${REPO_ROOT}/infrastructure-mgmt/main/terraform.tfvars" "terraform.tfvars.enc"
encrypt_one "${REPO_ROOT}/infrastructure-mgmt/main/targets.auto.tfvars" "targets.auto.tfvars.enc"
echo "Done. Commit secrets/*.enc; never commit secrets/.key or decrypted plaintext files."
