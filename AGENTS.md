# Repository Guidelines

## Project scope

MusicComments is a Go desktop application built with MyGo's native `ui` package.
It supports Windows amd64 and Linux amd64 builds. Windows is the currently
verified desktop runtime. Keep this architecture: do not introduce a web frontend,
WebView, JavaScript runtime, or additional service unless explicitly requested.

## Code standards

- Use idiomatic Go, run `gofmt`, and keep changes focused on the requested behavior.
- Keep upstream requests and selection logic in `internal/music`; keep window,
  layout, image, and presentation logic in the desktop application files.
- Perform network requests off the UI thread. Apply visible state changes through
  `Window.Update`, cancel work when the window closes, and discard stale results.
- Use request timeouts and bounded retries. Preserve useful errors internally and
  provide understandable Chinese messages in the interface.
- Treat comments and nicknames as untrusted plain text. Only recognized emoji
  labels may become bundled images; preserve unknown labels and original text.
- Reuse shared font and layout constants. Verify wrapped text, inline emoji,
  loading states, image fallbacks, and narrow windows when changing presentation.
- Avoid new dependencies unless they solve a concrete problem. Keep Go modules
  and the MyGo CLI pinned through `go.mod` and `go.sum`.
- Release automation scripts must use standard-library Python where practical.
  Pass Git data as arguments or file content; never evaluate commit messages as
  shell commands. Preserve Unicode and escape Markdown in generated notes.
- Do not include credentials, local settings, generated executables, dependency
  directories, logs, temporary screenshots, or test output in commits. Keep asset
  attribution accurate. README screenshots must reflect the real application.
- Do not add automatic release instructions to README; the user intentionally
  keeps that document focused on the application, usage, and compilation.

## Test standards

- Add or update meaningful regression tests for behavioral changes. Avoid tests
  that merely restate the implementation and unnecessary tests for trivial copy.
- Request tests must use local HTTP servers and synthetic data, not real user
  requests or credentials. Cover success, fallback, retry limits, and cancellation
  when affected by a change.
- Native UI tests use `ui.NewTester`. Check loading-state stability, read-only
  comments, image failures, stale asynchronous results, and relevant window sizes
  (800, 390, and 360 DIPs). Check rendered output for visual regressions; passing
  a build alone does not establish correct appearance.
- Before committing Go changes, run `go test ./...`, `go vet ./...`, and
  `go tool mygo vet`. For concurrency changes, also run `go test -race ./...`
  with a supported toolchain. On Windows, temporarily select MinGW/UCRT GCC if
  the default GCC is Cygwin, and restore the environment afterward.
- For release-note changes, run
  `python -m unittest discover -s .github/scripts -p "test_*.py"` and validate
  the workflow with `actionlint`. Test against isolated Git histories, including
  first releases, tag ranges, prereleases, and unstructured commit subjects.
- For packaging changes, verify both targets using
  `go tool mygo build -platform windows/amd64,linux/amd64` and inspect the files
  collected for publication. Compilation does not prove Linux desktop operation.
- Real-window and upstream checks are opt-in with `MUSIC_NATIVE_TEST=1`; they
  open a window and require network access. Report this separately from isolated
  tests. Never present synthetic fixtures as evidence of a live API request.
- Run `git diff --cached --check` before committing. Report verification results
  and any tests that could not be run without claiming they passed.

## Commit and publication standards

- Use Conventional Commit subjects: `type(optional-scope): concise description`.
  English or clear Chinese descriptions are accepted. Describe observable changes.
- Use `feat:` for a new capability, `fix:` for a bug fix, and `docs:` for
  documentation. Use `refactor:`, `perf:`, `test:`, `build:`, `ci:`, or `chore:`
  for other changes as appropriate. Keep unrelated changes in separate commits.
- Mark incompatible changes with `!` after the type/scope or a
  `BREAKING CHANGE:` paragraph in the commit body.
- Release notes group `feat`, `fix`, and `docs` into features, bug fixes, and
  documentation; other subjects appear under other changes. Write useful commit
  descriptions because they become public release notes. Do not put secrets or
  sensitive information in commit subjects or bodies.
- Inspect the working tree before staging. Stage only relevant files, preserve
  existing user changes, and do not amend published commits or force-push unless
  explicitly authorized.
- Push commits or create/push version tags only when requested. Before a release,
  align `mygo.json` with the version and check version examples that need updating.
  Use tags such as `v1.0.1` or `v1.1.0-rc.1`; do not move an existing release tag.
- A tag push triggers Windows/Linux builds and publication. The release-note
  generator uses commits after the preceding reachable version tag through the
  current tag; first releases include their complete history, excluding merges.
- Distinguish a successful push, a running workflow, and a published release.
  Verify the actual GitHub result before reporting publication as complete.
