## 1. Reconciler type & main wiring

- [x] 1.1 Change `EventRecorder` field type on `ModuleReleaseReconciler` (`internal/controller/modulerelease_controller.go:49`) from `record.EventRecorder` to `events.EventRecorder`; update import to `k8s.io/client-go/tools/events`
- [x] 1.2 Replace `mgr.GetEventRecorderFor("opm-controller")` with `mgr.GetEventRecorder("opm-controller")` in `cmd/main.go:242`

## 2. Reconcile package emission sites

- [x] 2.1 Update `internal/reconcile/modulerelease.go` to use the new signature `Eventf(regarding, related, type, reason, action, note, args...)` — pass `nil` for `related`
- [x] 2.2 Apply the action vocabulary from design D2 to all 9 emission sites (lines 98, 120, 263, 312, 340, 356, 392, 564, 573); replace any `Event(...)` calls with `Eventf(..., "%s", msg)` since the new interface only exposes `Eventf`
- [x] 2.3 Update the `params.EventRecorder` field type (and the local `recorder` variable around `:564`) wherever it is declared in the reconcile package

## 3. Test fixtures

- [x] 3.1 In `internal/controller/modulerelease_reconcile_test.go` and `modulerelease_controller_test.go`, replace every `record.NewFakeRecorder(N)` with `events.NewFakeRecorder(N)`
- [x] 3.2 Update event-channel assertions to match the new `"TYPE REASON ACTION MESSAGE"` format (search for `recorder.Events`, `<-recorder.Events`, and any substring matchers on event strings)
- [x] 3.3 Update test helper variables `recorder`, `resumeRecorder`, `noopRecorder` declarations to the new type
- [x] 3.4 Drop `k8s.io/client-go/tools/record` imports from test files where it becomes unused

## 4. Validation

- [x] 4.1 Run `goimports -w ./...` and confirm no stale `record` import survives anywhere in the repo
- [x] 4.2 Run `make fmt vet`
- [x] 4.3 Run `make lint` and confirm SA1019 is gone
- [x] 4.4 Run `make test` and confirm all envtest specs pass
- [ ] 4.5 Push branch; verify the GitHub Actions lint job is green on the change PR
