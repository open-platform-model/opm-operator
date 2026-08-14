# Tasks — typed-resolution-error-routing

> Draft scaffold — do not start before the retarget release (`v1.0.0-alpha.9`) is cut; see
> proposal § Impact / Ordering.

## 1. Classification

- [ ] 1.1 `classifyRenderError`: typed `errors.AsType` checks for `oerrors.IdentityError` and
      `*oerrors.UnresolvedDemandsError` route to `ResolutionFailedReason`, ahead of the
      `isResolutionError` string fallback (which stays for untyped loader errors).
- [ ] 1.2 Verify joined-error behavior: `UnresolvedDemandsError` joined with
      `*compile.UnmatchedComponentsError` must still classify (errors.As unwraps joins) — pin
      with a test against the library's actual join shape.
- [ ] 1.3 `modulepackage.go` `isResolutionErrorMsg`: align with the same typed checks, or record
      in design why the package path keeps string-only classification.

## 2. Tests

- [ ] 2.1 Unit tests: bare `IdentityError` → `ResolutionFailed`; `UnresolvedDemandsError`
      (bare and joined) → `ResolutionFailed`; `ErrPlatformNotReady` → `PlatformNotReady`
      unchanged; plain evaluation error → `RenderFailed` unchanged.
- [ ] 2.2 Check `moduleinstance_platform_gate_test.go:152`'s
      `NotTo(Equal(ResolutionFailedReason))` assertion still holds under the new routing.

## 3. Specs & gates

- [ ] 3.1 Identify the owning capability and author the delta spec (proposal § Capabilities).
- [ ] 3.2 `task dev:fmt dev:vet dev:lint dev:test`.
- [ ] 3.3 Scan commit messages / PR body for bare `@` tokens; plain co-author trailer only.
