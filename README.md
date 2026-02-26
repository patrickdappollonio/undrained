# undrained

A CLI tool that audits Kubernetes [PodDisruptionBudgets](https://kubernetes.io/docs/tasks/run-application/configure-pdb/) and flags configurations that block pod evictions during voluntary disruptions like node drains, cluster upgrades, and scaling operations.

## The problem

PodDisruptionBudgets (PDBs) protect workloads during voluntary disruptions, but misconfigured PDBs can silently prevent cluster operations entirely. Common pitfalls include:

- A PDB with `maxUnavailable: 0` that blocks all evictions permanently.
- A PDB with `minAvailable: 1` protecting a single-replica workload -- since Kubernetes must keep at least 1 pod available and there's only 1, it can never be evicted.
- A PDB with `maxUnavailable: 1` where one of two expected pods is already down -- the budget is already exhausted, so the remaining pod can't be evicted either.
- Orphaned PDBs whose selectors match no pods, indicating stale configuration.

These issues often surface at the worst time: during a node drain that hangs indefinitely, or a cluster upgrade that refuses to proceed.

`undrained` catches these problems before they block your operations.

## Installation

### Homebrew (macOS)

```bash
brew install patrickdappollonio/tap/undrained
```

### GitHub Releases

Download a prebuilt binary for your platform from the [Releases page](https://github.com/patrickdappollonio/undrained/releases/latest). Binaries are available for Linux, macOS, and Windows on amd64, arm, and arm64.

## Usage

```bash
# Analyze all PDBs across all namespaces
undrained

# Analyze a specific namespace
undrained -n kube-system

# Show all PDBs including healthy ones
undrained --all

# Wide output with full detail
undrained -o wide

# JSON output for scripting
undrained -o json

# Use a specific kubeconfig or context
undrained --kubeconfig /path/to/kubeconfig --context my-cluster
```

## Output formats

### Table (default)

Only problematic PDBs are shown by default. Use `--all` to include healthy ones.

```
NAMESPACE    NAME              ALLOWED  ISSUES
default      backend-pdb       0        minAvailable requires 3 pod(s) but only 3 of 3 pod(s) are healthy, allowing 0 disruptions
kube-system  critical-pdb      0        maxUnavailable is 0: no disruptions are ever allowed
staging      orphaned-pdb      0        PDB selector matches no pods; this PDB is not protecting any workload
```

### Wide

Adds `MIN-AVAIL`, `MAX-UNAVAIL`, `PODS`, and `HEALTHY` columns:

```
NAMESPACE    NAME              MIN-AVAIL  MAX-UNAVAIL  PODS  HEALTHY  ALLOWED  ISSUES
default      backend-pdb       3          -            3     3        0        minAvailable requires 3 pod(s) ...
kube-system  critical-pdb      -          0            5     5        0        maxUnavailable is 0: no disruptions ...
```

### JSON

```json
[
  {
    "namespace": "default",
    "name": "backend-pdb",
    "minAvailable": "3",
    "currentHealthy": 3,
    "expectedPods": 3,
    "disruptionsAllowed": 0,
    "issues": [
      {
        "type": "CurrentlyBlocking",
        "severity": "error",
        "message": "minAvailable requires 3 pod(s) but only 3 of 3 pod(s) are healthy, allowing 0 disruptions"
      }
    ]
  }
]
```

## What it detects

| Issue               | Severity | Description                                                                                                                                                                                                                                                              |
| ------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `AlwaysBlocking`    | error    | The PDB is structurally configured to never allow disruptions regardless of how many pods exist. Triggers for `maxUnavailable: 0`, `maxUnavailable: 0%`, and `minAvailable: 100%`.                                                                                       |
| `CurrentlyBlocking` | error    | The PDB currently allows zero disruptions given the actual pod count and health. For example, `minAvailable: 1` with a single pod, or `maxUnavailable: 1` when one pod is already unhealthy. Both integer and percentage values are evaluated against the current state. |
| `NoMatchingPods`    | warning  | The PDB's label selector doesn't match any pods in its namespace. The PDB is orphaned and likely stale.                                                                                                                                                                  |

## How it works

For each PDB in the cluster, `undrained`:

1. **Checks the spec for structurally broken configurations** -- values like `maxUnavailable: 0` that can never allow disruptions regardless of pod count.
2. **Resolves the PDB's label selector** to find all matching pods in the same namespace, including support for both `matchLabels` and `matchExpressions`.
3. **Counts healthy pods** (Running phase + Ready condition) among the matches.
4. **Computes allowed disruptions** using the same logic as the Kubernetes PDB controller: for `maxUnavailable`, it calculates `healthyPods - (totalPods - maxUnavailable)`; for `minAvailable`, it calculates `healthyPods - minAvailable`. Percentage values are resolved with ceiling rounding, matching Kubernetes behavior.
5. **Reports issues** when the computed disruptions allowed is zero.

## Kubernetes connection

The tool resolves a Kubernetes client using this precedence:

1. `--kubeconfig` flag (explicit path)
2. `KUBECONFIG` environment variable or `~/.kube/config`
3. In-cluster service account (when running inside a pod)

## Exit codes

| Code | Meaning                                              |
| ---- | ---------------------------------------------------- |
| `0`  | No problematic PDBs found                            |
| `1`  | One or more problematic PDBs detected                |
| `2`  | Runtime error (unable to connect, API failure, etc.) |

Exit code `1` on issues makes `undrained` suitable for CI pipelines and pre-upgrade checks:

```bash
# Fail a pipeline if any PDB would block operations
undrained || echo "Problematic PDBs detected, aborting upgrade"
```

## Flags

| Flag           | Short | Default | Description                            |
| -------------- | ----- | ------- | -------------------------------------- |
| `--kubeconfig` |       |         | Path to kubeconfig file                |
| `--context`    |       |         | Kubernetes context to use              |
| `--namespace`  | `-n`  | *(all)* | Namespace to analyze                   |
| `--output`     | `-o`  | `table` | Output format: `table`, `wide`, `json` |
| `--all`        | `-a`  | `false` | Show all PDBs including healthy ones   |

## License

MIT License. See [LICENSE](LICENSE) for details.
