# Releasing

## MyGo Music Plus

Use one tag for Plus desktop packages, signed update manifests and the Android APK. The Android build and APK signing key follow the existing procedure.

1. Set the version in `mygo.config.ts` and in `android/app/build.gradle.kts`. Increment Android's `versionCode` and add the release to `CHANGELOG.md`.
2. Run the tests and build the APK following the [Android build instructions](android/README.md#构建步骤). Sign with the same key as the previous Android release.
3. Push the release commit and matching tag to the Plus fork. The [desktop workflow](.github/workflows/plus-desktop.yml) builds Windows x64, macOS Intel/Apple Silicon and Linux x64/ARM64. On macOS an Intel runner builds both architectures with Apple's SDK and verifies the Mach-O architecture and bundle signature. Each job verifies the update signature and installs the archive into a disposable directory. Test-branch builds use disposable keys.
4. Download the five **tag run** artifacts. Create a draft release for that tag. Upload the installers/packages, every `update-<target>.json`, each corresponding `.tar.gz`, the install scripts and the APK. Keep all filenames from the build. Check shared install scripts for identical content before uploading one copy. Compare downloaded assets against the build digests, then publish as Latest. The current Plus update configuration uses signed full archives.

Tagged builds read `MYGO_UPDATER_PRIVATE_KEY` from the Plus repository secret. The matching public key is in [plus-update.pub](scripts/release/plus-update.pub). The local recovery copy, where provisioned, is `%LOCALAPPDATA%\MyGo Music Plus Signing\update-key.dpapi`, encrypted for the Windows user that created it.

The macOS verification packages use ad-hoc signing. For a Gatekeeper-notarized distribution, build on a Mac with a Developer ID Application identity and an Xcode notary keychain profile. Set `MYGO_PLUS_MAC_SIGN_IDENTITY` and `MYGO_PLUS_NOTARY_PROFILE` along with the Plus updater key and `MYGO_PLUS_DESKTOP=1`. Upload the DMG, update archive and manifest from that same signed build together.

## Upstream MyGo Music

Releases are built by [`.github/workflows/release.yml`](.github/workflows/release.yml). Installed apps update to a release once it's published.

### Cut an upstream release

1. Make sure `main` is clean and the tests pass:

   ```bash
   go test ./internal/...
   ```

2. Add a section for the new version at the top of [`CHANGELOG.md`](CHANGELOG.md), in the same style as the ones below it, and add its compare link at the bottom. Leave out website and test-only changes.

3. Bump `version` in [`mygo.config.ts`](mygo.config.ts).

4. Commit with the bare version as the message, tag it, and push both:

   ```bash
   git commit -am "0.1.6"
   git tag v0.1.6
   git push origin main v0.1.6
   ```

   The tag must match the version in `mygo.config.ts`, or the workflow stops. You can also run the workflow by hand (`gh workflow run release.yml`) instead of pushing a tag. It then tags the version in `mygo.config.ts` itself.

5. Wait for the workflow (`gh run watch`). It creates a **draft** release, using the version's `CHANGELOG.md` section as the notes, and uploads the macOS, Windows and Linux builds, their installers, the update manifests and the deltas from earlier versions.

6. Check the draft's notes and assets, then publish it:

   ```bash
   gh release edit v0.1.6 --draft=false --latest
   ```

   Installed apps only pick up the update once it's published.

7. Deploy the website. Its download pages read the latest release when it's built, so they show the old version until it's deployed again:

   ```bash
   bun run deploy
   ```

   This runs `vite build && wrangler deploy` in `website/`, and needs Wrangler signed in to the Cloudflare account (`bunx wrangler login`).

### Upstream workflow secrets

The workflow needs these repository secrets:

| Secret | Used for |
| --- | --- |
| `MACOS_CERTIFICATE`, `MACOS_CERTIFICATE_PASSWORD` | The Developer ID certificate (base64 `.p12`) that signs the macOS app |
| `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD` | Notarizing the macOS disk images |
| `MYGO_UPDATER_PRIVATE_KEY` | Signing the updates; its public key is `updates.publicKey` in `mygo.config.ts` |

### Upstream podcast caption libraries

The sherpa-onnx libraries are released separately, as the `sherpa-onnx-<version>` prerelease, and only change when that version changes. See the README.
