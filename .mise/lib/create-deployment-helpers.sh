#!/usr/bin/env bash
# Shared helper functions for create-deployment and its tests.
# Usage: source "$(dirname "$0")/../lib/create-deployment-helpers.sh"

# ── Display helpers ─────────────────────────────────────────────────

section_header() {
    echo ""
    echo "─── $1 ───────────────────────────────────────────────"
    echo ""
}

mask_secret() {
    local val="$1"
    local len=${#val}
    if [[ $len -le 8 ]]; then
        echo "****"
    else
        echo "${val:0:4}...${val: -4}"
    fi
}

# ── Validation helpers ──────────────────────────────────────────────

validate_aws_key_id() {
    [[ "$1" =~ ^AKIA[A-Z0-9]{16}$ ]]
}

validate_domain() {
    [[ -n "$1" && "$1" =~ \. ]]
}

validate_ssh_key_format() {
    [[ "$1" =~ ^ssh- ]]
}

extract_domain_root() {
    echo "$1" | rev | cut -d. -f1-2 | rev
}

derive_acme_email() {
    echo "admin@$(extract_domain_root "$1")"
}

# ── Interactive prompt helpers ──────────────────────────────────────

prompt_required() {
    local label="$1" default="${2:-}" explanation="${3:-}"
    if [[ -n "$explanation" ]]; then
        echo "  $explanation"
        echo ""
    fi
    local value=""
    while [[ -z "$value" ]]; do
        if [[ -n "$default" ]]; then
            read -rp "  $label [$default]: " value
            value="${value:-$default}"
        else
            read -rp "  $label: " value
        fi
        if [[ -z "$value" ]]; then
            echo "  (required)"
        fi
    done
    echo "$value"
}

prompt_optional() {
    local label="$1" default="${2:-}" explanation="${3:-}"
    if [[ -n "$explanation" ]]; then
        echo "  $explanation"
        echo ""
    fi
    local value=""
    if [[ -n "$default" ]]; then
        read -rp "  $label [$default]: " value
        value="${value:-$default}"
    else
        read -rp "  $label: " value
    fi
    echo "$value"
}

prompt_yesno() {
    local question="$1" default="${2:-n}"
    local suffix="[y/N]"
    [[ "$default" == "y" ]] && suffix="[Y/n]"
    read -rp "  $question $suffix: " answer
    answer="${answer:-$default}"
    [[ "$answer" =~ ^[Yy]$ ]]
}

detect_ssh_key() {
    local keys=()
    for f in ~/.ssh/id_*.pub; do
        [[ -f "$f" ]] && keys+=("$f")
    done
    if [[ ${#keys[@]} -eq 1 ]]; then
        echo "  Auto-detected: ${keys[0]}" >&2
        local default_key
        default_key=$(cat "${keys[0]}")
        read -rp "  SSH public key [${keys[0]}]: " SSH_INPUT
        if [[ -z "$SSH_INPUT" ]]; then
            echo "$default_key"
        else
            echo "$SSH_INPUT"
        fi
    elif [[ ${#keys[@]} -gt 1 ]]; then
        echo "  Multiple SSH keys found:" >&2
        for i in "${!keys[@]}"; do
            echo "    $((i+1)). ${keys[$i]}" >&2
        done
        read -rp "  Choose [1-${#keys[@]}] or paste key: " SSH_INPUT
        if [[ "$SSH_INPUT" =~ ^[0-9]+$ ]] && [[ "$SSH_INPUT" -ge 1 ]] && [[ "$SSH_INPUT" -le ${#keys[@]} ]]; then
            cat "${keys[$((SSH_INPUT-1))]}"
        else
            echo "$SSH_INPUT"
        fi
    else
        read -rp "  SSH public key: " SSH_INPUT
        echo "$SSH_INPUT"
    fi
}
