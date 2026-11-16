#!/usr/bin/env bash
# End-to-end demo against the current kubectl context.
# Needs the CRD installed (make install) and the controller running (make run).
set -euo pipefail
ns=demo
step() { echo; echo ">>> $*"; }
show() {
  kubectl -n $ns get deploymentwindow weekend-only
  p=$(kubectl -n $ns get deploy web -o jsonpath='{.spec.paused}')
  echo "web paused: ${p:-false}" \
       " annotation: $(kubectl -n $ns get deploy web -o jsonpath='{.metadata.annotations.deploywindow\.dev/paused-by}')"
  kubectl -n $ns get rs -l app=web -o custom-columns=REPLICASET:.metadata.name,IMAGE:.spec.template.spec.containers[0].image,READY:.status.readyReplicas
}

kubectl create namespace $ns --dry-run=client -o yaml | kubectl apply -f - >/dev/null

step "Create a prod Deployment (image pause:3.9)"
kubectl -n $ns create deployment web --image=registry.k8s.io/pause:3.9 --replicas=2
kubectl -n $ns label deployment web tier=prod
kubectl -n $ns rollout status deploy/web --timeout=180s

step "Apply a window that only allows weekend rollouts (so it is closed now, on a weekday)"
kubectl -n $ns apply -f - <<YAML
apiVersion: policy.deploywindow.dev/v1alpha1
kind: DeploymentWindow
metadata: {name: weekend-only}
spec:
  selector: {matchLabels: {tier: prod}}
  timeZone: Africa/Lagos
  windows:
    - {days: [Sat, Sun], start: "00:00", end: "23:59"}
YAML
sleep 3; show

step "Change the image to pause:3.10. The Deployment is paused, so no new ReplicaSet appears"
kubectl -n $ns set image deploy/web pause=registry.k8s.io/pause:3.10
sleep 8; show

step "Emergency fix: suspend the window. The controller resumes the Deployment and the rollout runs"
kubectl -n $ns patch deploymentwindow weekend-only --type=merge -p '{"spec":{"suspend":true}}'
sleep 3
kubectl -n $ns rollout status deploy/web --timeout=180s
show

step "Un-suspend: the window is closed again, so the Deployment is paused again"
kubectl -n $ns patch deploymentwindow weekend-only --type=merge -p '{"spec":{"suspend":false}}'
sleep 3; show

step "Delete the window: the finalizer resumes the Deployment before the object goes away"
kubectl -n $ns delete deploymentwindow weekend-only
sleep 2
p=$(kubectl -n $ns get deploy web -o jsonpath='{.spec.paused}')
echo "web paused: ${p:-false}"
kubectl -n $ns get deploymentwindow 2>&1 | head -1
