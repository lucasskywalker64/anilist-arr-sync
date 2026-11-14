# Code signing policy

This policy defines the signing and verification requirements for AniList-Arr-Sync
releases. The release's version, source commit, channel, and signing information
identify the particular build. A filename or a download location alone does not
establish its authenticity.

## Release channels

| Channel | Windows Authenticode | Checksums and publication |
| --- | --- | --- |
| Stable | Approved, timestamped signatures are required for the application, installer, uninstaller, and temporary setup copies. | Final SHA256 checksums are Minisign-signed before publication and downstream distribution. |
| Nightly | Omitted for development builds. | Final checksums remain Minisign-signed. The rolling prerelease records a distinct version and full source commit. |

## Signing service and responsibilities

Stable Windows signing uses SignPath with a production policy that requires human
approval. For releases sponsored through the SignPath Foundation open-source
program, the service credit is:

Free code signing provided by [SignPath.io](https://about.signpath.io/),
certificate by [SignPath Foundation](https://signpath.org/).

For those releases, SignPath Foundation is the certificate holder; the issuing
certificate authority and timestamp provider are identified in the file's
signature. Release notes and certificate details identify the applicable signing
information for each build. A signature does not guarantee that Windows will
suppress SmartScreen warnings.

Project responsibilities are assigned to:

- Trusted committer: [lucasskywalker64](https://github.com/lucasskywalker64).
- Reviewer for external contributions: [lucasskywalker64](https://github.com/lucasskywalker64).
- Release and signing approver: [lucasskywalker64](https://github.com/lucasskywalker64).

Maintainers review external changes before signing releases that contain them,
including changes to build scripts, dependencies, and workflows. Source-control
and signing accounts require multi-factor authentication. Changes to authorized
roles and members are recorded in this policy.

The maintainer controls release tags, production credentials, verification-key
publication, and signing approval. Automation can submit a request and wait for
its result; the CI identity has no signing-approval authority. Production key
generation, provisioning, rotation, and recovery belong to the maintainer.

## Build and signing process

Releases are built on GitHub-hosted runners from public source. Stable releases
use semantic version tags belonging to `main`; nightlies pin an exact `develop`
commit. Release sources must match the embedded commit and a clean checkout.
The frontend is built before Go compilation, and each application target is built
once per release.

Windows application executables are signed before entering portable archives and
installers. Inno Setup invokes signing for the installer, uninstaller, and its
temporary executable copies. The Windows service, tray, and detached update
worker use the same application executable, preserving its signature.

After signing, packaging, and installer verification, SHA256 hashes cover the final
archives, installers, and `release.json`. Minisign signs the exact `checksums.txt`
bytes. Artifact bytes are not changed after hashing. Uploaded assets are verified
before nightly promotion; production signing and final-byte checks precede release
publication and downstream package or container publication.

Missing configuration, failed or rejected signing, approval timeout, invalid
signatures, failed installer checks, or incorrect final hashes stop the release.
Automation does not substitute test keys or publish unsigned stable replacements.

## Verify a download

Obtain the expected Minisign public-key line through an authenticated channel
controlled by the maintainer. Establish trust in that line independently of the
download being checked. A key supplied only with an untrusted release cannot
authorize itself or replace a previously trusted key.

Download the chosen asset together with `checksums.txt` and
`checksums.txt.minisig` from the same release. With
[Minisign](https://jedisct1.github.io/minisign/) installed, verify the checksum
signature first. Replace `TRUSTED_PUBLIC_KEY_LINE` with the independently trusted
base64 public-key line:

```sh
minisign -Vm checksums.txt -P "TRUSTED_PUBLIC_KEY_LINE"
```

Continue only after successful verification. Calculate the downloaded file's hash
and compare it with the entry for that exact filename in the authenticated
checksum file. On Linux:

```sh
sha256sum FILE_NAME
```

On Windows PowerShell:

```powershell
Get-FileHash -LiteralPath .\FILE_NAME -Algorithm SHA256
```

For stable Windows executables, also inspect Authenticode. For a portable ZIP,
inspect the executable after extraction; for an installer, inspect the installer:

```powershell
Get-AuthenticodeSignature -LiteralPath .\FILE_NAME.exe |
  Format-List Status, SignerCertificate, TimeStamperCertificate
```

Require a valid signature and timestamp, and verify the certificate identity
against the expected release publisher information. A valid signature from an
unrelated publisher is not sufficient. Nightly Windows files omit Authenticode
signing; their checksum verification still applies.

## Built-in updates and source

Official application builds embed the trusted public key. The updater authenticates
the checksum signature and archive bytes, checks release identity, and verifies the
replacement process's version and commit. It never downloads a replacement
verification key from the release it is checking. Missing verification
configuration blocks updates.

Direct installations use the selected stable or nightly channel. Package-manager
installations delegate updates to their manager; containers update by image
replacement. Key changes require an authenticated maintainer trust decision.

Release notes, the application's **License and source** link, and `--license`
identify the corresponding source for the exact build. See [SOURCE.md](SOURCE.md)
for build and dependency-source instructions and [PRIVACY.md](PRIVACY.md) for data
handling. Installed applications do not contact SignPath; signing providers process
build artifacts as part of release distribution.
