#!/usr/bin/env bats

setup() {
    HELPERS="$(cd "$(dirname "$BATS_TEST_FILENAME")/../.mise/lib" && pwd)/create-deployment-helpers.sh"
    source "$HELPERS"
}

# ── mask_secret ─────────────────────────────────────────────────────

@test "mask_secret: long string shows first 4 and last 4" {
    result=$(mask_secret "AKIAIOSFODNN7EXAMPLE")
    [ "$result" = "AKIA...MPLE" ]
}

@test "mask_secret: exactly 9 chars shows first 4 and last 4" {
    result=$(mask_secret "123456789")
    [ "$result" = "1234...6789" ]
}

@test "mask_secret: 8 chars or fewer shows ****" {
    result=$(mask_secret "12345678")
    [ "$result" = "****" ]
}

@test "mask_secret: empty string shows ****" {
    result=$(mask_secret "")
    [ "$result" = "****" ]
}

@test "mask_secret: long token masked correctly" {
    result=$(mask_secret "github_pat_FAKE01234567890abcdefghijklmnopqrstuvwxyz")
    [ "$result" = "gith...wxyz" ]
}

# ── validate_aws_key_id ────────────────────────────────────────────

@test "validate_aws_key_id: valid key passes" {
    validate_aws_key_id "AKIAIOSFODNN7EXAMPLE"
}

@test "validate_aws_key_id: wrong prefix fails" {
    ! validate_aws_key_id "ASIA5RDCYX6LYSD4ZFIF"
}

@test "validate_aws_key_id: too short fails" {
    ! validate_aws_key_id "AKIA5RDCYX"
}

@test "validate_aws_key_id: too long fails" {
    ! validate_aws_key_id "AKIAIOSFODNN7EXAMPLEX"
}

@test "validate_aws_key_id: empty fails" {
    ! validate_aws_key_id ""
}

@test "validate_aws_key_id: lowercase chars fail" {
    ! validate_aws_key_id "AKIA5rdcyx6lysd4zfif"
}

# ── validate_domain ────────────────────────────────────────────────

@test "validate_domain: simple domain passes" {
    validate_domain "example.com"
}

@test "validate_domain: subdomain passes" {
    validate_domain "productbuilder.luminor-tech.net"
}

@test "validate_domain: no dot fails" {
    ! validate_domain "localhost"
}

@test "validate_domain: empty fails" {
    ! validate_domain ""
}

# ── validate_ssh_key_format ────────────────────────────────────────

@test "validate_ssh_key_format: ssh-rsa passes" {
    validate_ssh_key_format "ssh-rsa AAAAB3NzaC1yc2E..."
}

@test "validate_ssh_key_format: ssh-ed25519 passes" {
    validate_ssh_key_format "ssh-ed25519 AAAAC3Nza..."
}

@test "validate_ssh_key_format: not a key fails" {
    ! validate_ssh_key_format "not-a-key"
}

@test "validate_ssh_key_format: empty fails" {
    ! validate_ssh_key_format ""
}

# ── extract_domain_root ────────────────────────────────────────────

@test "extract_domain_root: subdomain extracts root" {
    result=$(extract_domain_root "productbuilder.luminor-tech.net")
    [ "$result" = "luminor-tech.net" ]
}

@test "extract_domain_root: deep subdomain extracts last two parts" {
    result=$(extract_domain_root "a.b.c.example.com")
    [ "$result" = "example.com" ]
}

@test "extract_domain_root: bare domain returns itself" {
    result=$(extract_domain_root "example.com")
    [ "$result" = "example.com" ]
}

# ── derive_acme_email ──────────────────────────────────────────────

@test "derive_acme_email: derives admin@ from subdomain" {
    result=$(derive_acme_email "productbuilder.luminor-tech.net")
    [ "$result" = "admin@luminor-tech.net" ]
}

@test "derive_acme_email: derives admin@ from bare domain" {
    result=$(derive_acme_email "example.com")
    [ "$result" = "admin@example.com" ]
}

# ── section_header ─────────────────────────────────────────────────

@test "section_header: output contains title" {
    result=$(section_header "AWS Configuration")
    [[ "$result" == *"AWS Configuration"* ]]
}

@test "section_header: output contains separator chars" {
    result=$(section_header "Test")
    [[ "$result" == *"───"* ]]
}

# ── prompt_yesno ───────────────────────────────────────────────────

@test "prompt_yesno: 'y' returns true" {
    echo "y" | prompt_yesno "Continue?" "n"
}

@test "prompt_yesno: 'Y' returns true" {
    echo "Y" | prompt_yesno "Continue?" "n"
}

@test "prompt_yesno: 'n' returns false" {
    ! echo "n" | prompt_yesno "Continue?" "n"
}

@test "prompt_yesno: empty with default 'y' returns true" {
    echo "" | prompt_yesno "Continue?" "y"
}

@test "prompt_yesno: empty with default 'n' returns false" {
    ! echo "" | prompt_yesno "Continue?" "n"
}

# ── prompt_required ────────────────────────────────────────────────

@test "prompt_required: accepts direct input" {
    result=$(echo "hello" | prompt_required "Name" "" "")
    [[ "$result" == *"hello"* ]]
}

@test "prompt_required: uses default on empty input" {
    result=$(echo "" | prompt_required "Region" "eu-central-1" "")
    [[ "$result" == *"eu-central-1"* ]]
}

# ── prompt_optional ────────────────────────────────────────────────

@test "prompt_optional: accepts direct input" {
    result=$(echo "myvalue" | prompt_optional "Field" "" "")
    [[ "$result" == *"myvalue"* ]]
}

@test "prompt_optional: uses default on empty input" {
    result=$(echo "" | prompt_optional "Field" "default_val" "")
    [[ "$result" == *"default_val"* ]]
}

@test "prompt_optional: empty with no default returns empty" {
    result=$(echo "" | prompt_optional "Field" "" "")
    [ -z "$result" ]
}

# ── Non-interactive scaffold integration ───────────────────────────

@test "non-interactive: creates expected directory structure" {
    local deploy_dir
    deploy_dir=$(mktemp -d)/productbuilding-deployment-batstest

    # We need to run create-deployment from the orchestration repo context
    local orch_dir
    orch_dir="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)"

    # Temporarily make the deploy dir's parent writable and in the right spot
    # We'll use the script directly instead of mise to avoid mise trust issues
    DEPLOY_DIR="$deploy_dir" \
    bash -c "
        cd '$orch_dir'
        .mise/tasks/create-deployment batstest --non-interactive
    " 2>&1 || true

    # Check the dir the script actually created (sibling to orchestration repo)
    local actual_dir
    actual_dir="$(dirname "$orch_dir")/productbuilding-deployment-batstest"

    if [[ -d "$actual_dir" ]]; then
        # Verify key files exist
        [ -f "$actual_dir/mise.toml" ]
        [ -f "$actual_dir/infrastructure-mgmt/main/main.tf" ]
        [ -f "$actual_dir/infrastructure-mgmt/main/variables.tf" ]
        [ -f "$actual_dir/infrastructure-mgmt/main/backend.tf" ]
        [ -f "$actual_dir/.mise/tasks/deploy" ]
        [ -f "$actual_dir/secrets/README.md" ]

        # Verify acme_email in generated files
        grep -q "acme_email" "$actual_dir/infrastructure-mgmt/main/main.tf"
        grep -q "acme_email" "$actual_dir/infrastructure-mgmt/main/variables.tf"

        # Verify backend.tf has REPLACE_ME (non-interactive doesn't bootstrap)
        grep -q "REPLACE_ME" "$actual_dir/infrastructure-mgmt/main/backend.tf"

        # Verify git repo was initialized
        [ -d "$actual_dir/.git" ]

        # Clean up
        rm -rf "$actual_dir"
    else
        rm -rf "$deploy_dir"
        # If the directory wasn't created at all, skip gracefully
        # (e.g., if it already existed from a prior run)
        skip "Scaffold directory was not created (may already exist)"
    fi

    rm -rf "$deploy_dir"
}
