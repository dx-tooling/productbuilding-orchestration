# Encrypted secrets (asymmetric)

Secret files are stored here **encrypted** so they can be committed to the repo safely. We use [age](https://github.com/FiloSottile/age) with **asymmetric** encryption:

- **Encryption** uses the **public key** in `public-key.txt` (committed). Anyone with the repo can encrypt.
- **Decryption** requires the **secret key** from 1Password, pasted into `.key` (gitignored).

## Setup (first time or new machine)

1. Install age: `brew install age` (macOS) or see [age releases](https://github.com/FiloSottile/age/releases).
2. Get the age secret key from 1Password and paste it into `secrets/.key`:
   ```bash
   # The key starts with AGE-SECRET-KEY-...
   pbpaste > secrets/.key   # macOS
   ```
3. From the **repo root**, decrypt all secrets:
   ```bash
   ./decrypt-secrets.sh
   ```

This produces the gitignored plaintext files used by mise tasks and OpenTofu.

## Encrypted files (committed)

| Encrypted file | Decrypts to | Purpose |
|----------------|-------------|---------|
| `secrets.yaml.enc` | `secrets.yaml` | AWS credentials, GitHub management PAT |
| `terraform.tfvars.enc` | `infrastructure-mgmt/main/terraform.tfvars` | OpenTofu variables (mgmt PAT, SSH key) |
| `targets.auto.tfvars.enc` | `infrastructure-mgmt/main/targets.auto.tfvars` | Per-target-repo credentials |

## Encrypting (after changing secrets)

```bash
./encrypt-secrets.sh
# Then commit the updated secrets/*.enc files
```

## Bootstrap: generating a new keypair

If starting from scratch (new project, rotated keys):

```bash
age-keygen -o secrets/.key
# Copy the printed public key into secrets/public-key.txt
# Store the secret key (contents of secrets/.key) in 1Password
```
