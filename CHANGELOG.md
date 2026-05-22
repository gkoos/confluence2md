# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.2](https://github.com/gkoos/confluence2md/compare/v0.1.1...v0.1.2) (2026-05-22)


### Bug Fixes

* trigger release 0.1.2 pipeline ([30da4e2](https://github.com/gkoos/confluence2md/commit/30da4e22904a98273f91346a993f3e3b11aa1435))
* trigger release 0.1.2 pipeline ([dc4cc5e](https://github.com/gkoos/confluence2md/commit/dc4cc5e8e0f7204633cfb60fa2ade7cc4683384e))
* trigger release 0.1.2 pipeline ([7d02795](https://github.com/gkoos/confluence2md/commit/7d0279590c981e6889ac43d7f56bd6aa1dda6676))

## [0.1.1](https://github.com/gkoos/confluence2md/compare/v0.1.0...v0.1.1) (2026-05-22)


### Bug Fixes

* remove unnecessary nil checks and fix channel syntax (golangci-lint) ([d3c3138](https://github.com/gkoos/confluence2md/commit/d3c313891aca7a34600747b6e2a33e8244535e52))
* satisfy lint errors and modernize workflow actions ([12f9789](https://github.com/gkoos/confluence2md/commit/12f978984e4c92033f1f542ae7869a1736bb78be))

## [Unreleased]

### Added

### Changed

### Fixed

### Deprecated

### Removed

### Security

## [0.1.0] - 2026-05-22

### Added
- Initial `confluence2md` CLI release.
- Full crawl mode (seed + max-depth traversal) and incremental updates mode.
- Shared traversal engine with selective page re-processing and clean-page reuse.
- Confluence storage-format to Markdown conversion pipeline.
- Two-pass local link rewrite and metadata graph persistence (`metadata.json`).
- Attachment download/reference rewrite and comment fetch/append support.
- Completed vs successful crawl checkpoints with updates summary metrics.
- Operational/internal documentation and broad unit test coverage.

[Unreleased]: https://github.com/gkoos/confluence2md/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/gkoos/confluence2md/releases/tag/v0.1.0
