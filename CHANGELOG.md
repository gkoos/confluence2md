# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

* Page metadata enrichment for RAG use cases: temporal fields (created_at, last_modified_at), authorship (created_by, last_modified_by with account IDs), and hierarchy (confluence_parent_id)
* User display name resolution with in-memory caching via Confluence REST API v1
* Comment author display name resolution (previously only showed account IDs)
* Comprehensive metadata.json structure with temporal, authorship, and hierarchy information

## [0.4.4](https://github.com/gkoos/confluence2md/compare/v0.4.3...v0.4.4) (2026-05-27)


### Bug Fixes

* dead code removed ([#30](https://github.com/gkoos/confluence2md/issues/30)) ([21d310e](https://github.com/gkoos/confluence2md/commit/21d310ecfb83ba2d8e06dbc2a7f2554cb0dffdb0))

## [0.4.3](https://github.com/gkoos/confluence2md/compare/v0.4.2...v0.4.3) (2026-05-27)


### Bug Fixes

* respect context cancellation in crawler workers ([#28](https://github.com/gkoos/confluence2md/issues/28)) ([06f6f66](https://github.com/gkoos/confluence2md/commit/06f6f66a1d110280f2096ee9fb4e054385ff1ef3))

## [0.4.2](https://github.com/gkoos/confluence2md/compare/v0.4.1...v0.4.2) (2026-05-27)


### Bug Fixes

* use alphanumeric space key instead of numeric ID ([#26](https://github.com/gkoos/confluence2md/issues/26)) ([2c98703](https://github.com/gkoos/confluence2md/commit/2c98703a0f466d3e6f7fe156aefaed5c0f109c19))

## [0.4.1](https://github.com/gkoos/confluence2md/compare/v0.4.0...v0.4.1) (2026-05-26)


### Bug Fixes

* **docs:** remove stale unreleased section from changelog ([e4dfdd8](https://github.com/gkoos/confluence2md/commit/e4dfdd8be21380b283cb4ad497bf7425c7480083))
* **docs:** remove stale unreleased section from changelog ([a5c97c1](https://github.com/gkoos/confluence2md/commit/a5c97c1bfdb22109254fb72ef56f72d5dbac40fd))

## [0.4.0](https://github.com/gkoos/confluence2md/compare/v0.3.0...v0.4.0) (2026-05-26)


### Features

* **crawl:** add configurable queue size and fail-fast saturation handling ([5e2f757](https://github.com/gkoos/confluence2md/commit/5e2f7572adfe0bf5b3afdeea97d3ed60d9ffd42d))
* **crawl:** add configurable queue size and fail-fast saturation handling ([eeae036](https://github.com/gkoos/confluence2md/commit/eeae03671c246823d39ef3d91183336bd8578efa))

## [0.3.0](https://github.com/gkoos/confluence2md/compare/v0.2.0...v0.3.0) (2026-05-26)


### Features

* **http:** add transport-level retry and request-level rate limiting ([389e351](https://github.com/gkoos/confluence2md/commit/389e351ab94aed3a97fbf7f83dcc79ba29e196c9))
* **rate-limit:** enforce crawl RPM at HTTP transport level ([f79fc68](https://github.com/gkoos/confluence2md/commit/f79fc6827864ae0a5107e563cd3414b7b3d95535))
* **retry:** wire retry config and transport ([56a18a8](https://github.com/gkoos/confluence2md/commit/56a18a8ab9a9a1582154e596b773e951c1d2cc39))


### Bug Fixes

* **rate-limit:** apply per-attempt host-scoped limiting with concurrency burst ([4b231ed](https://github.com/gkoos/confluence2md/commit/4b231edd4636b89d39c84cf0eb5abc24cdbcab0d))

## [0.2.0](https://github.com/gkoos/confluence2md/compare/v0.1.3...v0.2.0) (2026-05-23)


### Features

* add markdown front matter and start index output ([8d8b028](https://github.com/gkoos/confluence2md/commit/8d8b028a559434797e7d34b6b6fca5ed85a6e1db))
* **export:** add deterministic markdown front matter ([325375c](https://github.com/gkoos/confluence2md/commit/325375cfd845090edd7e1f5901b9b84e70ebabaa))
* **index:** generate minimal start index from metadata ([ef101b8](https://github.com/gkoos/confluence2md/commit/ef101b8066498f3d2e84863338988beaf3fc0315))
* **metadata:** persist seed_page_ids at metadata root ([cdecb4e](https://github.com/gkoos/confluence2md/commit/cdecb4e037ff7c2703f810f822d85d887160f10f))

## [0.1.3](https://github.com/gkoos/confluence2md/compare/v0.1.2...v0.1.3) (2026-05-22)


### Bug Fixes

* **ci:** use golangci-lint v2 with golangci-lint-action v9 ([de904f7](https://github.com/gkoos/confluence2md/commit/de904f731f931d250ffdf1be408eb6ed13ecfe94))

## [0.1.2](https://github.com/gkoos/confluence2md/compare/v0.1.1...v0.1.2) (2026-05-22)


### Bug Fixes

* trigger release 0.1.2 pipeline ([30da4e2](https://github.com/gkoos/confluence2md/commit/30da4e22904a98273f91346a993f3e3b11aa1435))
* trigger release 0.1.2 pipeline ([dc4cc5e](https://github.com/gkoos/confluence2md/commit/dc4cc5e8e0f7204633cfb60fa2ade7cc4683384e))
* trigger release 0.1.2 pipeline ([7d02795](https://github.com/gkoos/confluence2md/commit/7d0279590c981e6889ac43d7f56bd6aa1dda6676))

## [0.1.1](https://github.com/gkoos/confluence2md/compare/v0.1.0...v0.1.1) (2026-05-22)


### Bug Fixes

* remove unnecessary nil checks and fix channel syntax (golangci-lint) ([d3c3138](https://github.com/gkoos/confluence2md/commit/d3c313891aca7a34600747b6e2a33e8244535e52))
* satisfy lint errors and modernize workflow actions ([12f9789](https://github.com/gkoos/confluence2md/commit/12f978984e4c92033f1f542ae7869a1736bb78be))

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

[0.4.0]: https://github.com/gkoos/confluence2md/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/gkoos/confluence2md/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/gkoos/confluence2md/compare/v0.1.3...v0.2.0
[0.1.3]: https://github.com/gkoos/confluence2md/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/gkoos/confluence2md/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/gkoos/confluence2md/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/gkoos/confluence2md/releases/tag/v0.1.0
