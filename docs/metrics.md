# Metrics

## Provisioning Metrics

### `<resource>.provisioning.duration`

Megamon emits a standardized provisioning duration metric for managed resources (e.g. `megamon.nodepool.provisioning.duration`, `megamon.jobset.provisioning.duration`, `megamon.slice.provisioning.duration`).

*   **Introduced in**: `v1.1.1`
*   **Type**: Gauge
*   **Unit**: Seconds (s)
*   **Description**: Time spent provisioning a resource (GKE NodePool, JobSet, Slice).
*   **Labels**:
    *   `provisioning_state`: The state of provisioning. Possible values:
        *   `provisioning`: The resource is currently being provisioned. The value represents the time elapsed since provisioning started.
        *   `success`: The resource has successfully become ready. The value represents the total time taken to become ready.
        *   `failed`: The resource provisioning failed. The value represents the time elapsed until failure.
    *   Standard resource labels (e.g. `nodepool_name`, `jobset_name`, `tpu_accelerator`, `tpu_topology`, etc.).

#### Examples

##### 1. NodePool Metrics

**Resource still provisioning:**
```
megamon_alpha_nodepool_provisioning_duration_seconds{nodepool_name="tpu-test-pool", provisioning_state="provisioning", tpu_accelerator="tpu-v4-podslice", tpu_topology="2x2x1"} 45.123
```

**Resource successfully provisioned:**
```
megamon_alpha_nodepool_provisioning_duration_seconds{nodepool_name="tpu-test-pool", provisioning_state="success", tpu_accelerator="tpu-v4-podslice", tpu_topology="2x2x1"} 190.008
```

**Resource failed provisioning:**
```
megamon_alpha_nodepool_provisioning_duration_seconds{nodepool_name="tpu-test-pool", provisioning_state="failed", tpu_accelerator="tpu-v4-podslice", tpu_topology="2x2x1"} 120.045
```

##### 2. JobSet Metrics

**JobSet provisioning (in-flight):**
```
megamon_alpha_jobset_provisioning_duration_seconds{jobset_name="train-job", jobset_namespace="default", jobset_uid="abc-123", provisioning_state="provisioning", tpu_topology="2x2x1"} 32.504
```

**JobSet successfully ready:**
```
megamon_alpha_jobset_provisioning_duration_seconds{jobset_name="train-job", jobset_namespace="default", jobset_uid="abc-123", provisioning_state="success", tpu_topology="2x2x1"} 85.120
```