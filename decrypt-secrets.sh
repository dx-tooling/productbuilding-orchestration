#!/usr/bin/env bash
# Decrypt secrets/*.enc into their plaintext locations using the secret key in secrets/.key.
# The secret key is pasted from 1Password (see secrets/README.md).
set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SECRETS_DIR="${REPO_ROOT}/secrets"
KEY_FILE="${SECRETS_DIR}/.key"

if [ ! -f "$KEY_FILE" ]; then
    echo "Error: Secret key not found at ${KEY_FILE}" >&2
    echo "Paste the age secret key from 1Password into that file (see secrets/README.md)." >&2
    exit 1
fi

if ! command -v age >/dev/null 2>&1; then
    echo "Error: 'age' is not installed (brew install age)." >&2
    exit 1
fi

decrypt_one() {
    local enc_file="$1"
    local out_file="$2"
    if [ ! -f "$enc_file" ]; then
        echo "  (skip: $enc_file not found)"
        return 0
    fi
    local out_dir
    out_dir=$(dirname "$out_file")
    mkdir -p "$out_dir"
    if age -d -i "$KEY_FILE" -o "$out_file" "$enc_file" 2>/dev/null; then
        chmod 600 "$out_file"
        echo "  decrypted: $(basename "$enc_file") -> ${out_file#"$REPO_ROOT"/}"
    else
        echo "  (failed: $enc_file)" >&2
        rm -f "$out_file"
        return 1
    fi
}

echo "Decrypting secrets..."
decrypt_one "${SECRETS_DIR}/secrets.yaml.enc" "${REPO_ROOT}/secrets.yaml"
decrypt_one "${SECRETS_DIR}/terraform.tfvars.enc" "${REPO_ROOT}/infrastructure-mgmt/main/terraform.tfvars"
decrypt_one "${SECRETS_DIR}/targets.auto.tfvars.enc" "${REPO_ROOT}/infrastructure-mgmt/main/targets.auto.tfvars"
echo "Done."
