# Tasks — typed-resolution-error-routing

> Ordering gate satisfied: `v1.0.0-alpha.9` is tagged; work may start.

## 1. Classification

- [x] 1.1 Add the shared typed helper in `internal/reconcile` (design D1):
      `errors.AsType[oerrors.IdentityError]` (value type — no star) and
      `errors.AsType[*oerrors.UnresolvedDemandsError]` → resolution-class. Comment
      records the reachability asymmetry (`IdentityError` unreachable on the package
      path — no registry acquire; see design § Reachability by path).
- [x] 1.2 `classifyRenderError` (`internal/reconcile/moduleinstance.go`): call the helper
      after the `ErrPlatformNotReady` gate and ahead of the `isResolutionError` string
      fallback (which stays for untyped loader errors); route hits to
      `ResolutionFailedReason`.
- [x] 1.3 `renderErrorReason` (`internal/reconcile/modulepackage.go`): call the same helper
      after the `ErrUnsupportedKind` check and ahead of the `isResolutionErrorMsg` string
      fallback.
- [x] 1.4 Verify joined-error behavior: `*oerrors.UnresolvedDemandsError` joined with
      `*compile.UnmatchedComponentsError` (bare `errors.Join` from
      `library/opm/compile/module.go`, one `%w` wrap in the renderer) must still
      classify — pin with a test against the library's actual join shape so a library
      shape change fails tests instead of silently downgrading the reason.

## 2. Tests

- [x] 2.1 Unit tests (new — no classifier tests exist today): bare and wrapped
      `IdentityError` → `ResolutionFailed`; `UnresolvedDemandsError` (bare and joined,
      per 1.4) → `ResolutionFailed`; `ErrPlatformNotReady` → `PlatformNotReady`
      unchanged; plain evaluation error → `RenderFailed` unchanged; transform-failure
      join ("executing transforms") → `RenderFailed`. Cover both `classifyRenderError`
      and `renderErrorReason`.
- [x] 2.2 Check `internal/controller/moduleinstance_platform_gate_test.go:152`'s
      `NotTo(Equal(ResolutionFailedReason))` assertion still holds under the new routing
      (it must — typed checks sit after the platform gate, design D3).

## 3. Gates

- [x] 3.1 `task dev:fmt dev:vet dev:lint dev:test`.
- [x] 3.2 Scan commit messages / PR body for bare `@` tokens; plain co-author trailer only.
