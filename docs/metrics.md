# Metrics

## Nodepool Metrics

### `megamon.nodepool.provisioning.duration`

*   **Introduced in**: `v1.1.1`
*   **Type**: Gauge
*   **Unit**: Seconds (s)
*   **Description**: Time spent provisioning a GKE NodePool.
*   **Labels**:
    *   `nodepool_name`: The name of the NodePool.
    *   `tpu_accelerator`: The type of TPU accelerator (e.g., `tpu-v4-podslice`).
    *   `tpu_topology`: The TPU topology (e.g., `2x2x1`).
    *   `provisioning_state`: The state of provisioning. Possible values:
        *   `provisioning`: The NodePool is currently being provisioned. The value represents the time elapsed since provisioning started.
        *   `success`: The NodePool has successfully become ready. The value represents the total time taken to become ready.
        *   `failed`: The NodePool provisioning failed. The value represents the time elapsed until failure.

#### Examples

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