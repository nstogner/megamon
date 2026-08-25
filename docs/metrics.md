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
*   **Description**: Block and subblock topology and reservation labels are emitted across NodePool (e.g. `nodepool_up`, `nodepool_up_time_seconds`) and scheduling (`nodepool_job_scheduled`) metrics.
*   **Labels**:
    *   `block_name`: Reservation block name
    *   `block_id`: Physical topology block hash
    *   `subblock_name`: Reservation sub-block name
    *   `subblock_id`: Physical topology sub-block hash

### Object & Metric Mapping

| Resource | Applicable Metrics | Labels | Notes |
| :--- | :--- | :--- | :--- |
| **NodePool** | `nodepool_up`, `nodepool_up_time_seconds`, `nodepool_interruption_count`, etc. | `block_name`<br>`block_id`<br>`subblock_name`<br>`subblock_id` | From GKE node labels |
| **Scheduling** | `nodepool_job_scheduled` | `block_name`<br>`block_id`<br>`subblock_name`<br>`subblock_id` |  |

### Scrape Examples

```
megamon_nodepool_up{block_id="9a0e671424e45fd480ca172ad7a4e25d",block_name="tpu-block-0001",nodepool_name="tpu-pool-1",subblock_id="6ce4a464bd524e332477fad57c0875a5",subblock_name="tpu-block-0001-subblock-0002",tpu_accelerator="tpu7x",tpu_topology="4x4x4"} 1

megamon_nodepool_job_scheduled{block_id="9a0e671424e45fd480ca172ad7a4e25d",block_name="tpu-block-0001",job_name="train-job-0",jobset_name="train-jobset",nodepool_name="tpu-pool-1",subblock_id="6ce4a464bd524e332477fad57c0875a5",subblock_name="tpu-block-0001-subblock-0002"} 1
```
