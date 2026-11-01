# AGENTS.md — ClusterTool

These instructions apply to all AI coding agents working in this repository.
This file is the canonical, tool-agnostic source; `CLAUDE.md` imports it for
Claude Code. Update shared instructions here.

## Project context

ClusterTool is part of the TrueForge ecosystem. It is a Go CLI for generating
cluster repositories and managing Kubernetes clusters using Talos Linux,
Flux, GitOps, and SOPS-compatible secret workflows.

| Path | Responsibility |
| --- | --- |
| `cmd/` | Cobra commands and orchestration |
| `pkg/` | Implementation packages |
| `embed/generic/` | Templates used to generate cluster repositories |
| `partial_builds/precommit/` | Embedded standalone pre-commit checker |
| `.github/workflows/` | CI, test builds, and release workflows |

Read `go.mod` and the relevant workflows for current dependencies, Go
requirements, and build instructions. Keep the source repository separate
from generated cluster repositories.

## Making changes

- Read the relevant implementation and its callers before changing behavior.
- Keep changes focused on the requested task. Avoid unrelated refactoring,
  dependency updates, and formatting changes.
- Do not change CI, Renovate configuration, or repository-wide tooling unless
  required by the task.
- Follow existing Go, Cobra, logging, and error-handling conventions.
- Reuse existing helpers when their actual behavior fits the task.
- Preserve useful logging and propagate errors when an operation must fail.
- Run `gofmt` on modified Go files. Preserve unrelated formatting and comments.
- Preserve existing uncommitted work. Do not commit generated test artifacts
  or local build output.

## Templates and cluster workflows

- For template changes, check both the source templates and the resulting
  generated configuration.
- Preserve placeholders such as `DOTREPLACE...`, `REPLACEWITHCLUSTERNAME`, and
  `${...}`. Verify where substitution occurs before changing generation order.
- Validate Helm values against the relevant chart version and schema.
- Check shared Flux defaults before adding per-resource overrides. Keep CRDs
  and their instances in appropriately ordered Kustomizations.
- Preserve bootstrap ordering, dependency relationships, and resume behavior
  unless changing them is explicitly part of the task.

## Secrets and Git hooks

- Never expose or commit real credentials, private keys, or decrypted secrets.
  Use dummy secrets and temporary keys for tests.
- Distinguish working-tree content, staged content, and committed content
  when changing or testing encryption and Git hooks.
- Ensure encryption or staging failures can block a commit when required.
- Consider Windows and Linux paths, including spaces and non-ASCII characters.
- Do not run tests against a real cluster or perform apply, bootstrap,
  upgrade, reset, or wipe operations without explicit authorization.

## Validation

- Run focused tests for the changed behavior and relevant failure scenarios.
  Use the current CI workflows to determine additional checks.
- For complete test distributions, follow
  `.github/workflows/clustertool.test-build.yaml`: prepare the target platform's
  Talosctl assets and rebuild the pre-commit checker before building ClusterTool.
  A plain `go build` may not produce a complete test distribution.
- Consider both Windows and Linux for path, hook, and process changes.
  Distinguish runtime testing from cross-compilation.
- Report what was tested and any checks that could not be performed. Do not
  claim a successful bootstrap or platform coverage without evidence.

## Commits and pull requests

- Write commit messages, PR titles, and descriptions in English. Use
  Conventional Commits with the `clustertool` scope, for example
  `fix(clustertool): handle staging errors`.
- Follow `.github/PULL_REQUEST_TEMPLATE.md`, retaining its sections and
  checkboxes. Only check boxes supported by actual validation.
- Explain the problem, the resulting behavior, and relevant testing. State
  deliberately deferred issue scope; do not claim to close partially resolved
  issues. Describe required user action for breaking changes.
- For a PR written or prepared by an AI agent, put
  `🤖 This PR is AI generated.` in the Notes section instead of tool-specific
  attribution such as "Generated with ...".
- When opening or preparing a PR, tell the user about the required Contributor
  License Agreement and refer them to the current contribution information in
  `.github/README.md`. Only the user can sign the CLA; never sign on their behalf.
