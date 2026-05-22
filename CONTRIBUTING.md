# Contributing

Thank you for considering contributing to confluence2md!

## Prerequisites

- [Go 1.25+](https://go.dev/dl/)
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

## Sensitive data

Never commit a `config.yaml` containing real credentials. The file is gitignored. Use `config.example.yaml` as a reference.
