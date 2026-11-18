# Review rules

Read from the base branch by the verify skill and verifier agent. Only check items whose trigger matches the diff.
Add an item only when the same kind of bug has happened at least twice and can be spotted from a diff. Remove items that never trigger or keep false-flagging.

## Focus for this codebase
- **Never leave production frozen.** Every path that stops enforcing (open window, `suspend`, invalid spec, selector no longer matches, deletion) must resume what this window paused.
- **Only touch what we own.** The operator resumes a Deployment only if it carries `deploywindow.dev/paused-by: <this window>`. A manual pause is never undone.
- **Time is injected.** Schedule logic stays pure in `internal/schedule`; the reconciler reads `r.now()` (which falls back to `time.Now` when the test-only `Now` field is nil), never `time.Now()` directly.

## Checklist
| Trigger (visible in diff) | Check | Source |
|---|---|---|
| Stored data / schema / message shape changes | Old records without the new field still load | always-on |
| Logging or error messages touched | No secrets or user data in output | always-on |
| `api/v1alpha1/*_types.go` changed | `make manifests generate` was run and `config/crd`, `zz_generated.deepcopy.go` are in the diff; a new required field has a default or is `+optional` so existing objects still validate; README API tables updated | CRD schema |
| Status written (`Status().Update`/`Status().Patch`) | Status is written with `Update`, not a merge patch built from a copy; zero values such as `open: false` and `matchedDeployments: 0` must still be stored (fields have no `omitempty`) | README "Design decisions": merge patch dropped `open: false`, found by the e2e demo |
| A new branch in `Reconcile` that returns early | It resumes Deployments this window paused before returning, and still updates status / `Ready` | fail-open design |
| Code that sets or clears `spec.paused` | Pausing adds the `paused-by` annotation in the same update; resuming checks the annotation value equals this window's name | ownership design |
| `time.Now()` added under `internal/` | Use the injected clock (`r.now()`) or pass `now` into `schedule.Evaluate` | testability design |
| `internal/schedule` changed | Table tests cover the exact `start`/`end` boundary, a non-UTC zone and the next-transition value; consider a property test (next transition is in the future and flips `open`) | schedule logic |
| `+kubebuilder:rbac` markers changed | `config/rbac/role.yaml` regenerated; no verbs beyond what the code calls | least privilege |
