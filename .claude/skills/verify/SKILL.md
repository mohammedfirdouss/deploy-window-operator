---
name: verify-deploy-window-operator
description: How to boot and drive deploy-window-operator (Kubebuilder controller) to verify a change end to end on a kind cluster. Use with the global verify skill before committing.
---

# Verify deploy-window-operator

## Cheap checks
```bash
make lint        # golangci-lint v2.13.1 (custom build, see .custom-gcl.yml)
make build       # manifests + generate + fmt + vet + build bin/manager
make test        # unit (internal/schedule) + envtest (internal/controller), ~10s
git status       # after make: generated CRD/RBAC/deepcopy must already be committed
```

## Boot
```bash
kind create cluster --name dwo     # skip if it exists: kind get clusters
make install                       # install the CRD
make run > /tmp/dwo-manager.log 2>&1 & echo $! > /tmp/dwo-manager.pid   # ready when the log shows "Starting workers"
```

## Drive (main user flows)
1. **Full story:** `hack/demo.sh` → compare with `docs/demo-output.txt`:
   closed window pauses `demo/web` and adds `paused-by`; `set image` creates **no** new ReplicaSet;
   `suspend: true` resumes and the rollout completes; `suspend: false` pauses again; deleting the window resumes it.
   The script ends by **deleting** `weekend-only`; re-create it before steps 2–4:
   ```bash
   kubectl -n demo apply -f - <<'YAML'
   apiVersion: policy.deploywindow.dev/v1alpha1
   kind: DeploymentWindow
   metadata: {name: weekend-only}
   spec:
     selector: {matchLabels: {tier: prod}}
     timeZone: Africa/Lagos
     windows:
       - {days: [Sat, Sun], start: "00:00", end: "23:59"}
   YAML
   ```
2. **Status/print columns:** `kubectl -n demo get dw` → `OPEN`, `MATCHED`, `NEXT`, `SUSPENDED` all filled;
   `kubectl -n demo get dw weekend-only -o jsonpath='{.status.conditions}'` → `Ready=True/Applied`.
3. **Invalid spec fails open:** `kubectl -n demo patch dw weekend-only --type=merge -p '{"spec":{"timeZone":"Nope/Zone"}}'`
   → `Ready=False/InvalidSpec` and `demo/web` has `spec.paused` unset.
4. **Manual pause is kept:** pause a second labelled Deployment by hand (`kubectl rollout pause`), open/suspend the window → it stays paused.
5. **Kubebuilder e2e (what CI runs):** `make test-e2e` (creates kind cluster `deploy-window-operator-test-e2e`, builds and deploys the image).

## Teardown
```bash
kubectl delete ns demo --wait=false
pkill -P "$(cat /tmp/dwo-manager.pid)"; kill "$(cat /tmp/dwo-manager.pid)"   # make run and its child manager
kind delete cluster --name dwo   # and deploy-window-operator-test-e2e if make test-e2e ran
```

## Gotchas
- `hack/demo.sh` uses a **Sat/Sun-only** window so it is closed on a weekday. Run on a weekend and step 1 sees an open window and pauses nothing; change the days in the script's YAML for that run.
- Windows use `Africa/Lagos` (UTC+1). Check `status.nextTransition` against that zone, not local time.
- `make lint` runs golangci-lint with `GOTOOLCHAIN` set to the Go version in `go.mod` (`LINT_GOTOOLCHAIN`), like CI. If the linter runs under a newer Go it can't read stdlib export data and reports `typecheck` errors on every import (seen with Go 1.27.4 + v2.13.1). That happens when you call `bin/golangci-lint` directly (editor/IDE integration) or pass `LINT_GOTOOLCHAIN=local`; go through `make lint`.
- The `go` line in `go.mod` must keep a patch version (`go 1.26.0`, not `go 1.26`), or `make lint` fails with "go1.26 is a language version but not a toolchain version".
- Delete the window before deleting the namespace, or the finalizer can hold namespace deletion until the controller is running.
