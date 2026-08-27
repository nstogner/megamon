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
        *   `provisioning`: The resource is currently being provisioned. The value represents the elapsed time since provisioning started and increases every aggregation cycle.
        *   `success`: The resource has successfully become ready. The value represents the total time taken to become ready.
        *   `failed`: The resource provisioning failed. The value represents the time elapsed until failure.
    *   Standard resource labels (`nodepool_name`, `jobset_name`, `tpu_accelerator`, `tpu_topology`, etc.).

#### Alerting Use Case

Because `provisioning_state="provisioning"` steadily increases while a resource is in-flight, you can create proactive alerts for stuck provisioning workflows:

```promql
# Alert if any NodePool has been stuck in provisioning for more than 30 minutes
megamon_nodepool_provisioning_duration_seconds{provisioning_state="provisioning"} > 1800

# Alert if any JobSet has been stuck in provisioning for more than 15 minutes
megamon_jobset_provisioning_duration_seconds{provisioning_state="provisioning"} > 900
```

#### Scrape Examples

##### 1. NodePool Metrics

**Resource still provisioning:**
```
megamon_alpha_nodepool_provisioning_duration_seconds{nodepool_name="tpu-validation-pool-1763155065", provisioning_state="provisioning", tpu_accelerator="tpu-v4-podslice", tpu_topology="2x2x1"} 45.123
```

**Resource successfully provisioned:**
```
megamon_alpha_nodepool_provisioning_duration_seconds{nodepool_name="tpu-validation-pool-1763155065", provisioning_state="success", tpu_accelerator="tpu-v4-podslice", tpu_topology="2x2x1"} 160.010
```

**Resource failed provisioning:**
```
megamon_alpha_nodepool_provisioning_duration_seconds{nodepool_name="tpu-validation-pool-1763155065", provisioning_state="failed", tpu_accelerator="tpu-v4-podslice", tpu_topology="2x2x1"} 120.045
```

##### 2. JobSet Metrics

**JobSet provisioning (in-flight):**
```
megamon_alpha_jobset_provisioning_duration_seconds{jobset_name="tpu-jobset-validation", jobset_namespace="default", jobset_uid="452a4983-6cbb-494a-bd9e-93ebac89581a", provisioning_state="provisioning", tpu_topology="2x2x1"} 5.210
```

**JobSet successfully ready:**
```
megamon_alpha_jobset_provisioning_duration_seconds{jobset_name="tpu-jobset-validation", jobset_namespace="default", jobset_uid="452a4983-6cbb-494a-bd9e-93ebac89581a", provisioning_state="success", tpu_topology="2x2x1"} 18.415
```

**JobSet failed provisioning:**
```
megamon_alpha_jobset_provisioning_duration_seconds{jobset_name="tpu-jobset-failed-test", jobset_namespace="default", jobset_uid="c146a977-95b2-4cf4-bbb7-d10ad7ba9798", provisioning_state="failed", tpu_topology="2x2x1"} 70.287
```

## Topology and Reservation Labels

*   **Introduced in**: `v1.2.0`
*   **Description**: Topology placement and reservation labels (where available) for monitored resources.
*   **Labels**:
    *   `block_id`: Physical topology block hash (`cloud.google.com/gce-topology-block`).
    *   `subblock_id`: Physical topology sub-block hash (`cloud.google.com/gce-topology-subblock`).
    *   `block_name`: Reservation block name (`cloud.google.com/reservation-blocks`).
    *   `subblock_name`: Reservation sub-block name (`cloud.google.com/reservation-subblocks`).

### Object & Metric Mapping

| Object | Applicable Metrics | Labels | Notes |
| :--- | :--- | :--- | :--- |
| **GKE Nodepool** | `nodepool_up`, `nodepool_up_time_seconds`, `nodepool_interruption_count`, `nodepool_provisioning_duration_seconds`, etc. | `block_id`<br>`subblock_id`<br>`block_name`<br>`subblock_name` | |
| **NodePool Scheduling (Node)** | `nodepool_job_scheduled` | `block_id`<br>`subblock_id`<br>`block_name`<br>`subblock_name` | Extracted from scheduled Node. |
| **Slice** | `slice_up`, `slice_up_time_seconds`, `slice_interruption_count`, `slice_provisioning_duration_seconds`, etc. | `block_id`<br>`block_name` | Subblock labels omitted as a slice can span multiple subblocks and may change during a "repair". |
| **JobSet** | `jobset_up`, `jobset_up_time_seconds`, `jobset_interruption_count`, `jobset_provisioning_duration_seconds`, etc. | `block_id`<br>`block_name` | |

> **Note**: Labels are conditionally emitted only when present.
