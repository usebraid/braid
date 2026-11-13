# Release checklist

Manual checks around a release. Replace `<version>` with the release version without `v` (for example `0.1.0`) and `<tag>` with the tag (`v<version>`). Run the steps in order; each has a pass criterion.

## 1. Before merging the release PR

1. Open the release-please PR. Pass: the checks `check`, `band`, `e2e`, `image`, `snapshot`, `install-test`, `site` and `commits` are green (the snapshot and install tests build and install the real archives).
2. Read the diff of `CHANGELOG.md` and the version bumps: `.release-please-manifest.json`, `band/package.json`, `band/package-lock.json` and `band/.claude-plugin/plugin.json`. Pass: all show `<version>`, the changelog lists the expected changes, nothing private.

## 2. After the release workflow

3. `gh release view <tag> -R usebraid/braid --json assets --jq '.assets[].name'`. If assets are missing, the `goreleaser` job failed after release-please published the release: re-run it (until then `releases/latest` has no checksums). Pass: 6 `braid_*` archives (linux, darwin, windows; amd64, arm64), 2 `braid-server_*` archives, `checksums.txt`, `checksums.txt.sigstore.json`, and one `.sbom.json` per archive.
4. Download everything into a clean directory: `mkdir rel && cd rel && gh release download <tag> -R usebraid/braid`.

## 3. Signatures

5. In `rel/`:
   ```
   cosign verify-blob --bundle checksums.txt.sigstore.json --certificate-identity https://github.com/usebraid/braid/.github/workflows/release.yml@refs/heads/main --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
   cosign verify --certificate-identity https://github.com/usebraid/braid/.github/workflows/release.yml@refs/heads/main --certificate-oidc-issuer https://token.actions.githubusercontent.com ghcr.io/usebraid/braid-server:<version>
   ```
   Pass: both succeed.
6. Negative: `cp checksums.txt /tmp/c.txt && echo x >> /tmp/c.txt` and run the `verify-blob` command with `/tmp/c.txt` as the blob. Pass: it fails.

## 4. Checksums

7. In `rel/`: `sha256sum -c checksums.txt` (macOS: `shasum -a 256 -c checksums.txt`). Pass: every file `OK`.

## 5. Image

8. `docker buildx imagetools inspect ghcr.io/usebraid/braid-server:<version>`. Pass: lists `linux/amd64` and `linux/arm64`.
9. `trivy image ghcr.io/usebraid/braid-server@<digest from step 8> --severity HIGH,CRITICAL --exit-code 1`. Pass: no findings, or only ones the CI scan of the same release also accepted (compare with the `image` job).
10. In a scratch directory with `deploy/` and a filled `.env`: replace `build:` by `image: ghcr.io/usebraid/braid-server:<version>` for `braid`, then `docker compose up -d` and `curl -fsS http://localhost:8080/healthz`. Pass: `ok`; after logging in, the version in the web app footer (`GET /api/v1/about`) is `<version>`. Then `docker compose down -v`.

## 6. Homebrew

11. `brew install usebraid/tap/braid && braid version && brew test braid && brew uninstall braid`. Pass: `braid version` prints `<version>`, the test passes.

## 7. install.sh

12. In a clean macOS user account and a clean Linux user account: `curl -fsSL https://get.usebraid.dev | sh`, then `braid version`. Pass: prints `<version>`.
13. Tamper check against the real release: `scripts/test-install.sh rel` (the script points `BRAID_DOWNLOAD_BASE` at the downloaded copy in `rel/`, installs, then modifies an archive). Pass: exit 0, the tampered archive is rejected with a checksum error.

## 8. Windows

14. Download the zip, unzip, then in a terminal: `braid.exe version`, `braid login <server-url>`, `braid install`, `braid status`. Pass: version matches, `schtasks /Query /TN Braid` shows the task, `braid status` shows `login: ok` and exits 0.

## 9. Site

15. `curl -sI https://usebraid.dev` and open it in a browser. Pass: HTTP 200 over HTTPS with a valid certificate.
16. `curl -fsSI https://usebraid.dev/install.sh`. Pass: 200 and a text content type, not HTML. `curl -fsS https://get.usebraid.dev | head -3` shows the script start.
17. `cd site && npm ci --ignore-scripts && npm run build && node linkcheck.mjs dist --external`. Pass: exit 0.

## 10. Band

18. Run `band/RELEASE_CHECKLIST.md` (the `/context` zero-token comparison) with the released `braid` and the released plugin. Pass: its record section is filled in and the token numbers match.

## 11. Rollback

If a release is bad:

1. Mark the GitHub release as pre-release (`gh release edit <tag> --prerelease`) or delete its assets (`gh release delete-asset`) or the release.
2. Delete the image tag from GHCR (GitHub > Packages > `braid-server` > delete the version `<version>`), and `latest` if it points to it.
3. Revert the formula commit in `usebraid/homebrew-tap` (`git revert <sha>` there).
4. Fix forward: merge a `fix:` commit so release-please opens the next release PR. Do not reuse a tag.

## Record

| Date | Version | Result | Who |
|---|---|---|---|
|  |  |  |  |
