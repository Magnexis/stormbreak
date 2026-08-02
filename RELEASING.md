# Releasing Stormbreak

Stormbreak releases are Go module tags. No compiled archives are produced.

## Preconditions

- The release is made from the intended default-branch commit.
- `CHANGELOG.md` contains the release date and final version section.
- Public APIs, examples, README, architecture, and security guidance agree.
- The working tree contains no generated profiles, binaries, credentials, or
  unrelated changes.
- CI is green for the minimum Go version, current maintained Go versions,
  Windows, macOS, race detection, CodeQL, and fuzz smoke tests.

## Local verification

```bash
gofmt -l .
go test -count=1 -shuffle=on ./...
go test -race -count=1 ./...
go vet ./...
go test -run '^$' -fuzz '^FuzzBackoffBounds$' -fuzztime=10s .
go test -run '^$' -fuzz '^FuzzRetryAfterDelay$' -fuzztime=10s ./httpretry
go test -run '^$' -bench . -benchmem .
```

`gofmt -l .` must produce no output. Benchmark movement should be investigated,
but benchmark numbers are not release gates unless a regression is understood
to affect production behavior.

## Tagging

Use an annotated semantic-version tag from the verified commit:

```bash
git tag -a v0.1.0 -m "stormbreak v0.1.0"
git push origin v0.1.0
```

The release workflow repeats formatting, tests, race detection, vet, and fuzz
smoke checks before creating GitHub release notes. It rejects non-semantic tags
and an unexpected module path. It does not publish binaries.

## Post-release verification

After the module proxy observes the tag, verify it from a clean temporary module:

```bash
go mod init example.com/stormbreak-release-check
go get github.com/magnexis/stormbreak@v0.1.0
go list -m github.com/magnexis/stormbreak
```

If a release contains a defect, publish a new patch version. Do not move or
rewrite a published Go module tag.
