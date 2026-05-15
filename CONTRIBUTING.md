# Contributing to ODH Observability Operator

## Getting Started

1. Fork and clone the repository.
2. Install prerequisites:
   - Go 1.22+
   - make
   - controller-gen (installed automatically by `make` targets)
3. Run the tests to verify your setup:

```bash
make test
```

## Code Style

- Follow standard Go conventions.
- Run `go fmt ./...` and `go vet ./...` before committing. These are also run automatically by `make test`.
- Use [Conventional Commits](https://www.conventionalcommits.org/) for all commit messages:
  - `feat:` -- new functionality
  - `fix:` -- bug fixes
  - `test:` -- adding or updating tests
  - `docs:` -- documentation changes
  - `refactor:` -- code restructuring without behavior changes
  - `chore:` -- build, CI, or tooling changes

## Testing

- All new code must have tests.
- Use the standard library `testing` package with table-driven test patterns.
- Use fake clients from `sigs.k8s.io/controller-runtime/pkg/client/fake` for unit tests.
- No external test frameworks (e.g. Ginkgo, Testify) -- keep dependencies minimal.
- Run tests:

```bash
make test          # Full pipeline (codegen + fmt + vet + tests)
make unit-test     # Tests only (faster iteration)
```

## PR Process

1. Create a branch from `main`.
2. Make your changes with conventional commit messages.
3. Ensure all tests pass locally (`make test`).
4. Open a pull request against `main`.
5. In the PR body, describe what was changed and include any manual testing steps you performed.
6. CI will run `make test` automatically.

## PR Checklist

Before submitting, verify the following:

- [ ] Tests pass (`make test`)
- [ ] Conventional commit messages used
- [ ] Manual testing described in the PR body
- [ ] No hardcoded credentials or secrets
