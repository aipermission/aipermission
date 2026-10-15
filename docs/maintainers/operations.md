# Maintainer Operations And Recovery

This runbook documents the official repository, security, and release recovery
boundaries. It contains no credentials or recovery codes.

## Current Ownership

AIPermission currently has one maintainer. That is a known project risk, not an
implicit grant of release authority to contributors or automation.

| Boundary | Primary owner | Backup owner | Recovery authority |
| --- | --- | --- | --- |
| Repository, branch protection, and GitHub Security Advisories | `@hakanekin` | Unassigned | A verified owner of the `aipermission` GitHub organization |
| Release tags and GitHub releases | `@hakanekin` | Unassigned | A verified organization owner after account recovery |
| `@aipermission/mcp` npm package | `@hakanekin` | Unassigned | npm organization owner/support account recovery |
| GHCR container images and attestations | GitHub Actions from the official repository | Unassigned | Repository organization owner after workflow review |
| Security triage and coordinated disclosure | `@hakanekin` | Unassigned | A future named security reviewer recorded here and in `CODEOWNERS` |
| Connector-specific review | `@hakanekin` | Unassigned | A future named owner recorded in `CODEOWNERS` |

Do not add a backup owner to this table until that person has accepted the role,
enabled strong account security, and received only the minimum repository or
registry permissions needed for it.

## Recovery Readiness

The primary maintainer must keep account recovery material outside the
repository. At minimum, periodically verify:

- GitHub and npm use strong 2FA and current recovery methods.
- The GitHub organization and npm organization recovery email addresses are
  current and independently accessible.
- Official releases can be reproduced from a tagged repository commit;
  no local-only source tree is required.
- npm publishing uses the official workflow with provenance when available.
- GHCR images are produced only by the pinned official workflow and can be
  verified with the commands in `RELEASE_CHECKLIST.md`.
- A recent encrypted AIPermission database backup can be restored without
  exposing its password or gateway secret to a registry or backup provider.

Never commit account recovery codes, npm tokens, signing material, database
passwords, or registry credentials to this repository or its issue tracker.

## Repository Security Settings

GitHub settings are separate from workflow source. CI cannot prove these
administrator-controlled settings are enabled; audit their live values after
workflow changes and before publication. Apply changes to this repository,
not to every organization repository by default.

| Setting | Required state |
| --- | --- |
| Default branch ruleset | Active; PR required; deletion and force pushes blocked; no bypass actors. |
| Required status checks | Every `required_checks[].name` in `scripts/verification-policy.json`, bound to the GitHub Actions app; require the PR branch to be up to date. |
| Secret scanning | Enabled, including push protection. This does not detect every secret format. |
| Private vulnerability reporting | Enabled; keep the report link in `SECURITY.md` usable. |
| Dependabot security updates | Enabled to open security PRs, not to merge them automatically. |
| Actions policy | Selected reviewed action paths only; full-length SHA pinning required; no blanket allowance for GitHub-owned or verified Marketplace actions. |
| Default workflow token | Read-only; approving PR reviews disabled. Workflows request additional permissions explicitly only where needed. |
| Auto-merge | Disabled. Never merge bot-authored dependency commits directly. |

Read-only checks from an authenticated maintainer terminal:

```bash
gh api repos/aipermission/aipermission/rules/branches/main
gh api repos/aipermission/aipermission/rules/branches/main --paginate --jq '.[].ruleset_id' |
  sort -u |
  while read -r ruleset_id
  do
    gh api "repos/aipermission/aipermission/rulesets/$ruleset_id"
  done
gh api repos/aipermission/aipermission/actions/permissions
gh api repos/aipermission/aipermission/actions/permissions/selected-actions
gh api repos/aipermission/aipermission/actions/permissions/workflow
gh api repos/aipermission/aipermission/private-vulnerability-reporting
gh api repos/aipermission/aipermission/automated-security-fixes
gh api repos/aipermission/aipermission --jq '{security_and_analysis, allow_auto_merge}'
node -p 'require("./scripts/verification-policy.json").required_checks.map(check => check.name).join("\n")'
```

Keep the required check names and their GitHub Actions integration binding
aligned with the policy and successful check runs on the exact source commit.
Inspect each ruleset's `bypass_actors`, `enforcement`, and `conditions` in the
detail responses; the effective-rules endpoint alone does not expose bypasses.
When changing check identities, plan the ruleset migration before merging;
never remove a required check merely because a PR is red. Preserve the
existing PR, deletion, force-push and bypass settings during updates.

Review every external `uses:` reference in `.github/workflows/` and local
composite actions against the selected-action allowlist before introducing a
new action. Allowlist patterns may use `@*` for a reviewed action path so
maintainer-authored version updates remain possible; SHA enforcement still
rejects tag and branch references. Inspect bot PRs as patch references,
reproduce accepted changes in maintainer-authored commits, and close the bot
PRs as superseded only after the replacement is delivered.

Verify the private report form with a non-owner account periodically. API
readback alone does not prove a reporter can submit a report or that a second
human will triage it. Hardware-backed MFA, account recovery and independent
backup ownership remain separate operator responsibilities.

## Emergency Security Release

Use this path only for a confirmed vulnerability that risks credential
exposure, unintended connector execution, permission bypass, or unsafe backup
recovery.

1. Keep exploit details in a private GitHub Security Advisory.
2. Identify the last affected release and the smallest safe patch.
3. Implement the patch on the advisory's private fork or another access-limited
   branch. Avoid unrelated dependency or feature changes.
4. Add a regression test that fails before the fix and passes after it.
5. Run `make -f Makefile release-check` plus the connector-specific manual smoke needed for
   the affected boundary.
6. Review the exact source commit, generated release notes, package contents,
   container provenance, and database migration impact.
7. Publish in this order: protected source commit, release tag, GitHub release,
   container images, then MCP package. Verify every artifact resolves to the
   same source commit before disclosure.
8. Publish the advisory and upgrade guidance. State whether users must rotate
   MCP tokens, connector credentials, Vault values, backup credentials, or the
   database password.
9. Record follow-up hardening separately; do not hide unrelated work in the
   emergency patch.

If the maintainer account is unavailable, automation must not invent a release
or publish from an unverified fork. Recover the GitHub/npm organization through
their official account-recovery process, verify branch and workflow integrity,
then resume at step 3.

## Adding A Backup Maintainer

Before granting release or security authority:

1. Review sustained contributions and judgment around local-only deployment,
   credentials, approvals, and connector isolation.
2. Assign the narrowest GitHub/npm role that satisfies the responsibility.
3. Require strong 2FA and confirmed recovery methods.
4. Update this ownership matrix and `.github/CODEOWNERS` in one reviewed pull
   request.
5. Run one supervised release dry run and one security-response tabletop drill.

The project should prefer explicit unassigned ownership over a nominal backup
who cannot safely perform the role.

## Continuity Limits

Automated tests, AI-assisted reviews and reproducible releases do not constitute
an independent human maintainer or remove the current single-person bus factor.
Keep backup roles unassigned until an actual person accepts them. Review this
matrix during each release and after any account or permission change; a release
checklist cannot prove account recovery will succeed. If continuity is lost,
use verified organization/registry recovery rather than granting an agent or
unverified fork emergency publishing authority.
