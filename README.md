<!--
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
SPDX-License-Identifier: Apache-2.0
-->

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
2. **Helm release secret** → for Helm‑managed objects, the injector looks for `sh.helm.release.v1.<release>.vN` secrets and reads `global.greenhouse.ownedBy` from the highest‑versioned release. This is the primary source for Greenhouse‑managed releases.
3. **Helm owner ConfigMap** → if no `ownedBy` value is found in the release secret, the injector looks up a per‑release ConfigMap:

   * `owner-of-<release>` in the **release namespace** (primary source)
   * `early-owner-of-<release>` (fallback, e.g., for pre‑release/bootstrapping)
4. **Static rules** → if no owner ConfigMap exists, a regex‑based rules file maps Helm release **name/namespace** to `supportGroup` and optional `service`.
5. **Owner traversal** → for non‑Helm or generated resources, the injector follows `ownerReferences` upward (and handles special cases) until owner data is found.

Once found, labels are merged into the object's `metadata.labels`. If the object contains a **pod template** (Deployment/StatefulSet/DaemonSet/Job/CronJob), the same labels are merged into `.spec.template.metadata.labels`.

> The annotation `<prefix>/support-group-datasource` records where labels were sourced from. Possible values: `helm-release-secret`, `owner-info` (ConfigMap), or `static-config`.

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
* `e2e/` — KinD‑based end‑to‑end tests with a test Helm chart, covering release‑secret precedence, ConfigMap fallback, label stickiness, and owner‑reference traversal.

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

### Environment Variables

The owner-label-injector can be configured via environment variables:

#### Label Configuration
* `LABEL_PREFIX` (default: `ccloud`) — prefix for all injected labels
* `SUPPORT_GROUP_SUFFIX` (default: `support-group`) — suffix for the support group label
* `SERVICE_SUFFIX` (default: `service`) — suffix for the service label
* `DATA_SOURCE_ANNOTATION_SUFFIX` (default: `support-group-datasource`) — suffix for data source annotation

#### Helm Configuration
* `OWNER_CONFIGMAP_PREFIX` (default: `owner-of-`) — prefix for primary owner ConfigMaps
* `OWNER_CONFIGMAP_FALLBACK_PREFIX` (default: `early-owner-of-`) — prefix for fallback owner ConfigMaps
* `SUPPORT_GROUP_DATA_KEY` (default: `support-group`) — key in ConfigMaps for support group data
* `SERVICE_DATA_KEY` (default: `service`) — key in ConfigMaps for service data

#### Traversal Configuration
* `VICE_PRESIDENT_ANNOTATION_KEY` (default: `vice-president/claimed-by-ingress`) — annotation key for TLS cert ingress discovery

#### Static Rules
* `STATIC_RULES` — JSON array of static rules for mapping Helm releases to owner data

When no owner ConfigMap exists for a Helm release, static rules provide regex-based mapping from Helm release name/namespace to `supportGroup` and optional `service`.

Example JSON for `STATIC_RULES`:
```json
{
  "rules": [
    {
      "helmReleaseName": ".*",
      "helmReleaseNamespace": "kubernikus",
      "supportGroup": "containers",
      "service": "optional-service-name"
    }
  ]
}
```

Each rule supports:
* `helmReleaseName` (regex pattern) — matches against the Helm release name
* `helmReleaseNamespace` (regex pattern) — matches against the Helm release namespace
* `supportGroup` (string, required) — the support group to assign
* `service` (string, optional) — the service to assign

Rules are evaluated in order and the first matching rule is used.

### Binary flags

The manager binary supports:

* `-metrics-bind-address` — default `:8080`.
* `-health-probe-bind-address` — default `:8081`.

The webhook server listens on port `9443` inside the pod (see `config/manager/controller_manager_config.yaml`).

---

## Using the CLIs

### labeller

Scan the cluster and backfill labels where owner data can be discovered.

```sh
# dry run across all namespaces and cluster‑level APIs
kubectl run -it --rm labeller --image=$IMG -- \
  --namespace=all \
  --cluster-level-apis=true \
  --namespaced-apis=true \
  --dry-run
```

Flags:

* `--namespace` (default `all`; comma‑separated for multiple)
* `--cluster-level-apis` (default `true`)
* `--namespaced-apis` (default `true`)
* `--summary` (print only a summary table)
* `--dry-run` (do not patch resources)

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
* ClusterRole `manager-role` (in `config/rbac/role.yaml`) grants `get,list,patch,watch` on `*/*` — necessary for discovery, caching, and patching. Review and tighten for your environment.
* Pod security context drops **all** capabilities and disables privilege escalation in provided manifests.

---

## Testing

* **Unit tests** (Ginkgo/Gomega) cover the admission logic: run with `go test ./...`.
* **End‑to‑end**: `make setup-e2e` creates a KinD cluster with the webhook deployed, then `make e2e` runs Ginkgo tests covering release‑secret precedence, ConfigMap fallback, no‑source, label stickiness, and owner‑reference traversal. E2E tests also run in CI via GitHub Actions.

---

## Local development

Requirements: Go (per `go.mod`), `kustomize`, `kubectl`, `helm`, `kind` (for e2e).

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

## Support, Feedback, Contributing

This project is open to feature requests/suggestions, bug reports etc. via [GitHub issues](https://github.com/cloudoperators/owner-label-injector/issues). Contribution and feedback are encouraged and always welcome. For more information about how to contribute, the project structure, as well as additional contribution information, see our [Contribution Guidelines](CONTRIBUTING.md).

## Security / Disclosure
If you find any bug that may be a security problem, please follow our instructions at [in our security policy](https://github.com/cloudoperators/owner-label-injector/security/policy) on how to report it. Please do not create GitHub issues for security-related doubts or problems.

## Code of Conduct

We as members, contributors, and leaders pledge to make participation in our community a harassment-free experience for everyone. By participating in this project, you agree to abide by its [Code of Conduct](https://github.com/SAP/.github/blob/main/CODE_OF_CONDUCT.md) at all times.

## Licensing

Copyright 2025 SAP SE or an SAP affiliate company and owner-label-injector contributors. Please see our [LICENSE](LICENSE) for copyright and license information. Detailed information including third-party components and their licensing/copyright information is available [via the REUSE tool](https://api.reuse.software/info/github.com/cloudoperators/owner-label-injector).
