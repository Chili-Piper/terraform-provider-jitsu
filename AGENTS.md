# Repository Guidelines

## Project Structure & Module Organization
- `main.go`: provider entrypoint for Terraform/OpenTofu.
- `internal/provider/`: provider schema/config and acceptance tests (`*_test.go`).
- `internal/resources/`: Terraform resources (`function`, `stream`, `destination`, `link`) plus shared helpers.
- `internal/client/`: Jitsu API client and DB-assisted soft-delete recovery logic.
- `examples/main.tf`: end-to-end local usage example.
- `GNUmakefile` and `.github/workflows/ci.yaml`: canonical local and CI commands.

## Build, Test, and Development Commands
- `make build`: compile `terraform-provider-jitsu` in repo root.
- `make install`: build and print reminder for `dev_overrides` setup.
- `make test`: run all Go tests (`go test ./... -v`).
- `make testacc`: run acceptance tests with `TF_ACC=1` and extended timeout.
- `go vet ./...`: static checks (also enforced in CI).
- Single test example: `TF_ACC=1 go test ./internal/provider -v -run TestAccFunction_basic`.

## Coding Style & Naming Conventions
- Language: Go (`go 1.24` in `go.mod`). Always run `gofmt` on changed files.
- Keep packages lowercase; use idiomatic Go naming (`CamelCase` exported, `camelCase` internal).
- Resource files are organized by Jitsu object (`internal/resources/function.go`, etc.); follow that pattern for new resources.
- Terraform schema attributes use `snake_case` with explicit `tfsdk` tags.
- Prefer small, explicit payload maps and clear diagnostic errors in CRUD handlers.

## Testing Guidelines
- Framework: `terraform-plugin-testing` acceptance-style tests in `internal/provider/`.
- Test names follow `TestAcc<Resource>_<scenario>` (for example, `TestAccFunction_basic`).
- Acceptance tests require:
  - `JITSU_CONSOLE_URL`
  - `JITSU_USERNAME`
  - `JITSU_PASSWORD`
  - `JITSU_DATABASE_URL`
- Run `make test` before every PR; run `make testacc` when changing provider/resource/client behavior.

## Commit & Pull Request Guidelines
- Commit subjects in this repo are short, imperative, and descriptive (for example, `Add ...`, `Cleanup ...`).
- Keep commits focused by concern (provider schema, client behavior, resource logic, tests).
- PRs should include:
  - concise summary of behavior changes,
  - linked issue/context,
  - test evidence (commands run and key results),
  - updated example/config docs when schema or UX changes.
- CI must pass: build, `go vet`, and `go test`.

## Security & Configuration Tips
- Do not commit real tokens or database URLs.
- Prefer environment variables for secrets and local `~/.tofurc` or `~/.terraformrc` `dev_overrides` during development.
