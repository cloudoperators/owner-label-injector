[![REUSE status](https://api.reuse.software/badge/github.com/cloudoperators/owner-label-injector)](https://api.reuse.software/info/github.com/cloudoperators/owner-label-injector)

# Owner Label Injector

A Kubernetes mutating admission webhook and companion CLIs that ensure every relevant resource carries standardized **owner labels**:

* `ccloud/support-group`
* `ccloud/service`

These labels make ownership **auditable and enforceable** across clusters: for incident routing, cost allocation, SLO roll‑ups, and cleanup automation.

---

## Table of contents

* [How it works](#how-it-works)
* [Components](#components)
* [Installation](#installation)

  * [Option A — via Helm chart (recommended)](#option-a--via-helm-chart-recommended)
  * [Option B — via Kustomize from this repo](#option-b--via-kustomize-from-this-repo)
* [Configuration](#configuration)

  * [Static rules (regex mapping)](#static-rules-regex-mapping)
  * [Binary flags](#binary-flags)
* [Using the CLIs](#using-the-clis)

  * [labeller](#labeller)
  * [label-remover](#label-remover)
* [Observability](#observability)
* [Security & RBAC](#security--rbac)
* [Testing](#testing)
* [Local development](#local-development)
* [FAQ](#faq)
* [License](#license)

---

## How it works

At admission time, the webhook inspects the incoming object and determines its **owner data** using the following precedence:

1. **Existing labels on the object** → if both owner labels are already present and valid, the request is allowed unchanged.
2. **Helm release metadata** → for Helm‑managed objects (`app.kubernetes.io/managed-by=helm` and `meta.helm.sh/*` annotations), the injector looks up a per‑release ConfigMap:

   * `owner-of-<release>` in the **release namespace** (primary source)
   * `early-owner-of-<release>` (fallback, e.g., for pre‑release/bootstrapping)
3. **Static rules** → if no owner ConfigMap exists, a regex‑based rules file maps Helm release **name/namespace** to `supportGroup` and optional `service`.
4. **Owner traversal** → for non‑Helm or generated resources, the injector follows `ownerReferences` upward (and handles special cases) until owner data is found.

Once found, labels are merged into the object’s `metadata.labels`. If the object contains a **pod template** (Deployment/StatefulSet/DaemonSet/Job/CronJob), the same labels are merged into `.spec.template.metadata.labels`.

> The annotation `ccloud/support-group-datasource` is set when labels are sourced from the Helm owner ConfigMap (value: `owner-info`).

### Special cases handled during traversal

* `vice-president/claimed-by-ingress=ns/name` annotation → treat that Ingress as the owner.
* `VerticalPodAutoscalerCheckpoint` → follows `spec.vpaObjectName` to the owning VPA.
* PVCs generated from StatefulSet `volumeClaimTemplates` → derive the StatefulSet owner.
* Old `extensions/v1beta1` owner refs are normalized to current APIs.

---

## Components

* **Webhook** — admission handler exposed at `/mutate-generic` (see `config/webhook/manifests.yaml`).
* **labeller** — CLI to backfill or recheck labels across existing resources (ideal for periodic jobs).
* **label-remover** — CLI to remove owner labels across resources (useful for migrations/cleanup).

Supporting directories:

* `api/v1/` — admission handler and helpers (`generic_labeller.go`, `utils.go`, `config.go`).
* `config/` — Kustomize overlays for the manager, webhook, RBAC, cert‑manager, and a CronJob that can run the `labeller` periodically.
* `e2e/` — minimal Helm chart and script that deploys sample workloads and verifies labelling end‑to‑end.

---

## Installation

### Option A — via Helm chart (recommended)

Use the curated chart that installs the injector with sane defaults:

* **Chart:** `system/owner-label-injector`
* **Owner info producer:** pair application releases with the helper chart `common/owner-info` which publishes `owner-of-<release>` ConfigMaps the injector consumes.

> Links:
>
> * system/owner-label-injector: [https://github.com/sapcc/helm-charts/tree/master/system/owner-label-injector](https://github.com/sapcc/helm-charts/tree/master/system/owner-label-injector)
> * common/owner-info: [https://github.com/sapcc/helm-charts/tree/master/common/owner-info](https://github.com/sapcc/helm-charts/tree/master/common/owner-info)

### Option B — via Kustomize from this repo

Use the included overlays under `config/`.

1. Build your image and set `IMG`:

   ```sh
   export IMG=<registry.example.com>/owner-label-injector:<tag>
   ```

2. Deploy:

   ```sh
   # set image and apply
   make deploy IMG=$IMG
   ```

3. (Optional) Enable the scheduled backfill job by applying `config/manager/cronjob.yaml` and mounting your rules ConfigMap (see below).

To remove:

```sh
make undeploy
```

> Certificates are provisioned via cert‑manager in `config/certmanager/`. The webhook service is `webhook-service` in the `owner-label-injector-system` namespace by default.

---

## Configuration

### Static rules (regex mapping)

Provide a ConfigMap named `owner-label-injector-config` with an embedded `config.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: owner-label-injector-config
  namespace: owner-label-injector-system
data:
  config.yaml: |
    rules:
      - helmReleaseName: ".*"
        helmReleaseNamespace: "kubernikus"
        supportGroup: containers
        # service: optional
```

Each rule:

* `helmReleaseName` (regex)
* `helmReleaseNamespace` (regex)
* `supportGroup` (string, required)
* `service` (string, optional)

Mount this ConfigMap into the controller (and into the `labeller` CronJob if used). The controller reads its path from the `-config` flag.

### Binary flags

The manager binary supports:

* `-config` — path to the YAML config described above.
* `-metrics-bind-address` — default `:8080`.
* `-health-probe-bind-address` — default `:8081`.

The webhook server listens on port `9443` inside the pod (see `config/manager/controller_manager_config.yaml`).

---

## Using the CLIs

### labeller

Scan the cluster and backfill labels where owner data can be discovered.

```sh
# dry run across all namespaces and cluster‑level APIs
kubectl -n owner-label-injector-system create configmap owner-label-injector-config \
  --from-file=config.yaml=./config/manager/configmap.yaml

kubectl run -it --rm labeller --image=$IMG -- \
  --namespace=all \
  --cluster-level-apis=true \
  --namespaced-apis=true \
  --dry-run \
  --config=/owner-label-injector-config/config.yaml
```

Flags:

* `--namespace` (default `all`; comma‑separated for multiple)
* `--cluster-level-apis` (default `true`)
* `--namespaced-apis` (default `true`)
* `--summary` (print only a summary table)
* `--dry-run` (do not patch resources)
* `--config` (path to the same YAML config as the controller)

### label-remover

Remove owner labels across resources (careful!).

```sh
# example: remove labels for a specific support group only
kubectl run -it --rm label-remover --image=$IMG -- \
  --namespace=all \
  --support-group=dev \
  --dry-run=false
```

Flags:

* `--namespace` (default `all`)
* `--cluster-level-apis` (default `true`)
* `--namespaced-apis` (default `true`)
* `--support-group` (value to remove; if empty, **all** support-group values are removed)
* `--summary`
* `--dry-run`

---

## Observability

* Prometheus `ServiceMonitor` manifests live in `config/prometheus/monitor.yaml` and scrape `/metrics` on the manager over HTTPS.
* Health and readiness probes bind to `:8081` (see `controller_manager_config.yaml`).

---

## Security & RBAC

* The webhook’s `MutatingWebhookConfiguration` is configured with `failurePolicy: Ignore` so API requests don’t fail if the injector is unavailable.
* ClusterRole `manager-role` (in `config/rbac/role.yaml`) grants `get,list,patch` on `*/*` plus `get,list,watch` on ConfigMaps — necessary for discovery and patching. Review and tighten for your environment.
* Pod security context drops **all** capabilities and disables privilege escalation in provided manifests.

---

## Testing

* **Unit tests** (Ginkgo/Gomega) cover the admission logic: run with `go test ./...`.
* **End‑to‑end**: `e2e/e2e.sh` deploys a tiny Helm chart with/without owner info and checks that workloads end up labelled correctly.

---

## Local development

Requirements: Go (per `go.mod`), `kustomize`, `kubectl`.

Common tasks:

```sh
# vendor deps
make vendor

# print manifests with your image injected
make print IMG=$IMG

# deploy / undeploy
make deploy IMG=$IMG
make undeploy
```

To run the injector against your current kube‑context, build and deploy with an image that your cluster can pull. For backfilling without the webhook, run the `labeller` CLI as a Job or one‑off Pod (see examples above).

---

## FAQ

**Why do I need the `common/owner-info` chart?**

It emits a per‑release ConfigMap (`owner-of-<release>`) with the owning team/service. The injector reads this for every Helm‑managed object and applies owner labels automatically.

**What happens if there’s no owner info?**

Static regex rules (if provided) are consulted. Otherwise, the webhook allows the request unchanged.

**Does it label generated pods?**

Yes. For workload kinds that embed a pod template (Deployment/StatefulSet/DaemonSet/Job/CronJob), labels are also injected into `.spec.template.metadata.labels`.

**Will it overwrite existing labels?**

It merges labels. If owner labels are already present and match the computed owner, no change is made.

---

## License

This project is licensed under the Apache 2.0 license. See the `LICENSES/` directory for details.
