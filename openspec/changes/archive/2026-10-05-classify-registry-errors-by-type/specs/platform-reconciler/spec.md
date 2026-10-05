## MODIFIED Requirements

### Requirement: Transient failures retry faster than semantic failures

The reconciler SHALL requeue a transient failure on a short interval and every other failure on the long stalled-recheck interval. A failure is transient when its error chain holds the library's typed registry fetch failure (`*oerrors.FetchError`, of any `Kind`) and none of the typed terminal causes, the same classification the ModuleInstance and ModulePackage reconcilers use (`reconcile-backoff`, "Registry fetch failures are transient wherever they occur"), or when it holds `context.DeadlineExceeded`. The shared classification picks the Platform's requeue interval only: the Platform's condition stays `Stalled=True` for every failure, unchanged by this rule. The reconciler SHALL classify by type, never by message text; an unrecognized cause SHALL default to the long interval. The condition the failure sets is unchanged by its class.

#### Scenario: Transient cause retries quickly

- **WHEN** the build fails because a registry fetch failed: the registry is unreachable or times out, does not hold a pinned catalog version, or refuses the credentials
- **THEN** the reconcile requeues on the short interval

#### Scenario: Semantic or unknown cause retries slowly

- **WHEN** the build fails for a cause that is not a registry fetch failure, for example a generated module that does not build, a stored registry entry without its version, or an unclassifiable cause
- **THEN** the reconcile requeues on the long stalled-recheck interval
