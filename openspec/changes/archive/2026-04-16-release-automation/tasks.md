## 1. Release-Please Configuration

- [x] 1.1 Create `release-please-config.json` with release-type `go`, package name `poc-controller`, bump-minor-pre-major enabled, changelog sections grouped by commit type
- [x] 1.2 Create `.release-please-manifest.json` with initial version `0.1.0` for root path `"."`

## 2. GitHub Actions Workflow

- [x] 2.1 Create `.github/workflows/release.yml` with release-please action triggered on push to `main`, using the config and manifest files from task group 1

## 3. Validation

- [x] 3.1 Verify workflow YAML is valid (correct syntax, proper action versions, required permissions)
- [x] 3.2 Verify config and manifest JSON are valid and reference correct paths
