# Changelog

Entries below describe unreleased source changes. Earlier history has not been
reconstructed here.

## [Unreleased]

### Fixed

- Upgrade bundled KaTeX to 0.18.11 for ordinary and Mermaid math, rejecting
  inherited rendering options such as a polluted prototype's trust setting.

### Added

- CI rebuilds the embedded browser client and compares generated bundle,
  stylesheet, chunk, and notice files with the committed assets.
- Git-diff regression coverage for replacements, additions, deletions, staged
  fallback, unstaged precedence, and unchanged files in temporary repositories.

### Changed

- Document that an explicit non-loopback listener exposes the preview and
  editing API without authentication, and advise a trusted network or an
  authenticated proxy.
