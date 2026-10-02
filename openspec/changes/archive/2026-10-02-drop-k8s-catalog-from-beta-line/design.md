## Decisions

The edit is documentation and configuration only. The spec requirement is modified in place with
the single list entry removed; every scenario is kept verbatim, because OpenSpec 1.12 refuses a
MODIFIED requirement that drops a main-spec scenario.

The claim and platform-generation tests that use `opmodel.dev/catalogs/k8s@v1` at
`1.0.0-alpha.2` as a real second catalog are untouched: no first-party test catalog is published
under `testing.opmodel.dev/catalogs/`, so their replacement waits on an owner decision.
