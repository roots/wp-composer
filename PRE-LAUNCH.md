# PRE-LAUNCH

Track remaining public-release decisions before launch.

## Open Decision (High Priority)

- [ ] **Public repo hygiene for infra-sensitive files**
  Files: `deploy/ansible/inventory/hosts/production.yml`, `deploy/ansible/group_vars/production/vault.yml`
  Concern: Production host/IP and encrypted vault are committed. Not always a blocker, but increases operational risk and targeting surface.

  Decision options:
  - Keep as-is (accept risk with strong vault password handling + rotation policy).
  - Keep Ansible in-repo, but move runtime inventory/vault to GH Secrets and inject at deploy time.
  - Keep encrypted vault in-repo, but remove/sanitize production inventory details from the public repo.

## GH Actions Follow-up (If Chosen)

- [ ] Prototype GH Actions workflow to inject Ansible vault content at runtime from GitHub Secrets (without committing runtime vault material).
- [ ] Document operational flow: secret creation, workflow usage, and local fallback path.
- [ ] Decide whether to keep encrypted vault in-repo after prototype:
  - Keep encrypted vault in repo (accepted risk with controls), or
  - Move to runtime-only secret injection for public repo.

## Completed Hardening

- [x] Deploy now always builds a fresh binary (no stale artifact reuse).
- [x] Deploy no longer ignores service restart failures.
- [x] `/downloads` now enforces request body size limits.
- [x] Admin login now has in-app brute-force throttling.
- [x] Production bind/proxy hardening applied (`127.0.0.1:8080` + explicit Caddy upstream).
