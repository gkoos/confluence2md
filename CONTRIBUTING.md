# Contributing

Thank you for considering contributing to confluence2md!

## Prerequisites

- [Go 1.24+](https://go.dev/dl/)
- [Task](https://taskfile.dev/installation/) — cross-platform build runner

## Building

```sh
task build
```

The binary is written to `bin/confluence2md.exe` (Windows) or `bin/confluence2md` (Linux/Mac).

## Running tests

```sh
task test
```

## Linting

```sh
task lint
```

This requires [golangci-lint](https://golangci-lint.run/usage/install/) to be installed.

## Submitting changes

1. Fork the repository and create a branch from `main`.
2. Make your changes and add tests where appropriate.
3. Ensure `task test` and `task lint` pass.
4. Open a pull request with a clear description of the change.

## Commit message convention

Use Conventional Commits so automated versioning/changelog generation can classify changes correctly:

- `feat:` for new features (minor release bump)
- `fix:` for bug fixes (patch release bump)
- `feat!:` or `BREAKING CHANGE:` for breaking changes (major release bump)
- `chore:`, `docs:`, `refactor:`, `test:`, etc. for non-feature/non-fix work

Examples:

- `feat: add updates-mode checkpoint reporting`
- `fix: handle missing reused page artifact`
- `feat!: change metadata schema for checkpoint model`

## Release process

Releases are automated via GitHub Actions:

1. CI runs on pull requests and pushes to `main`.
2. Release Please updates or opens a release PR from conventional commits.
3. Merging the release PR creates a new tag and GitHub release.
4. GoReleaser builds and uploads `confluence2md` binaries for supported platforms.

## Sensitive data

Never commit a `config.yaml` containing real credentials. The file is gitignored. Use `config.example.yaml` as a reference.
