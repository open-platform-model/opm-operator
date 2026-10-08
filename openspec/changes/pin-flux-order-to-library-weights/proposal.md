## Why

The operator applies with Flux's `ApplyAllStaged` (`internal/apply/apply.go:110`), which cuts the set into stages and sorts every stage by Flux's own kind order. No operator path sorts by the library's kind weights (`opm/k8s/object`): the operator calls none of `object.Sort`, `object.Stages` and `object.Weight`. The two orders agree today only because library v1.0.0-beta.7 changed its weight table to follow Flux `ssa` v0.77.0. 0012:D5:R1 allows an engine's staging to refine the library's order and never to contradict it.

Nothing in the operator checks that rule. The library's own guard (`opm/k8s/object/flux_order_test.go`) compares its table with a hand-copied list of Flux's order, because Flux is banned from the library. Its comment names "a planned opm-operator test, comparing Flux's own staged order with object.Weight through the Flux module itself" as what catches a Flux bump. That test does not exist. A Flux bump or a library bump can therefore make the operator and the CLI apply the same set in opposite orders, and every check stays green.

## What Changes

- A new test in `internal/apply` builds a set of objects that covers every kind class of both orders, puts it in the order Flux's staged apply gives it, and fails for every pair of kinds that Flux applies in the opposite order to the library's weights. Flux's order is read by calling the pinned Flux module: its exported stage rules and its exported sorter. It is not copied as literals, so a Flux bump that moves a kind fails this test on the bump PR.
- A second test in `test/integration/apply` runs the operator's real `apply.Apply` against the test API server, records the order of the writes, and checks that order against the library weights and against Flux's stage rules. It covers what the first test has to assume: the precedence of Flux's stages and the options the operator passes.
- The failure message of both tests names the pairs, the two pinned modules (the comparison test with their versions from `go.mod`), and what the maintainer does next. It says that the test and its kind list are not edited to get a pass.
- One requirement is added to the `ssa-apply` spec for the rule the tests guard.
- No production code changes. No API type, controller or reconcile phase changes. Not in this change: the order of prune and deletion (the operator walks the inventory in recorded order there and does not use Flux's order or the weights), any pre-sort by library weights before the apply, the library's guard and its literals, the CLI's pinned weight table.

SemVer: none. The change is test-only and ships no release (`test` is a hidden type in `release-please-config.json`). After GA it would also be no release. PR title: `test(apply): fail when Flux's staged apply order contradicts the library weights`.

Complexity (Principle VII): two test files and no new dependency (`github.com/fluxcd/pkg/ssa`, `github.com/fluxcd/cli-utils`, the library and `testify` are direct requirements already). The alternative is to trust two release notes to stay in step by hand.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `ssa-apply`: one added requirement, "The staged apply order never contradicts the library's kind weights".

## Impact

- Tests: a new file `internal/apply/flux_order_test.go`; a new file in `test/integration/apply`.
- Specs: `openspec/specs/ssa-apply/spec.md` gains one requirement at archive.
- Code, APIs, CRDs, docs, dependencies: none.
- Bump PRs: a Flux bump or a library bump that makes the two orders disagree now fails `task dev:test`. That is the purpose. The fix is in the library (its weight table and the Flux literals of its own guard), never in the operator's test.
