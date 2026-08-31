# Governance

AIPermission is a maintainer-led open source project. Hakan Ekin
([@hakanekin](https://github.com/hakanekin)) is the current lead maintainer and
has final responsibility for project scope, security boundaries, releases, and
repository administration.

The project currently has no backup maintainer or independent security
reviewer. This is a known bus-factor risk. Current ownership, recovery
boundaries, the emergency release procedure, and the criteria for adding a
backup maintainer are documented in
[Maintainer Operations And Recovery](docs/maintainers/operations.md).

## Decisions

- Day-to-day fixes and small features are decided through focused pull requests.
- Changes to the local-only, single-user, connector, credential, permission, or
  approval boundaries require maintainer review and documentation updates.
- Significant architectural decisions are recorded under `docs/adr/` before or
  with their implementation.
- Security reports follow the private process in [SECURITY.md](SECURITY.md), not
  public issue discussion.

The project is pre-1.0. Breaking changes are allowed when they remove technical
debt or strengthen the security model, but they must be called out in the
changelog and release notes. Unless a security advisory says otherwise, only
the latest release line receives fixes.

## Contributions

Contributors retain copyright in their work and submit it for distribution
under the repository's `AGPL-3.0-only` license. Opening a pull request does not
grant merge, release, or maintainer authority.

Maintainer status may be offered after sustained, trustworthy contributions
that preserve the project principles and demonstrate sound judgment around
credentials, execution, approvals, and local-only deployment. The lead
maintainer grants or removes that role and records ownership in `CODEOWNERS`.

## Releases

Only a maintainer may merge release pull requests, create tags and GitHub
releases, publish the MCP package, or publish official container images. The
release process is defined in [RELEASE_CHECKLIST.md](RELEASE_CHECKLIST.md).
Confirmed urgent security fixes also follow the emergency procedure in
[Maintainer Operations And Recovery](docs/maintainers/operations.md).
