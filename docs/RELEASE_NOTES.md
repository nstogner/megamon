# Release Notes

## v1.2.0

### New features and improvements
* Add block and subblock topology and reservation labels to metrics:
    * `block_id`: Physical topology block hash (`cloud.google.com/gce-topology-block`)
    * `subblock_id`: Physical topology sub-block hash (`cloud.google.com/gce-topology-subblock`)
    * `block_name`: Reservation block name (`cloud.google.com/reservation-blocks`)
    * `subblock_name`: Reservation sub-block name (`cloud.google.com/reservation-subblocks`)
    * **NB:** `subblock_id` and `subblock_name` apply to NodePool and NodePool Scheduling metrics, while `block_id` and `block_name` apply across NodePool, Slice, JobSet, and metrics. See [docs/metrics.md](metrics.md) for details.

### Fixes
* Fix mock GCS client data race condition by adding deep-copy helpers (`EventRecords.Clone()`, `CloneEventRecordsMap()`) and mutex synchronization

### Example of new block topology metrics
```
megamon_nodepool_job_scheduled{block_id="9a0e671424e45fd480ca172ad7a4e25d",block_name="tpu-block-0001",job_name="train-job-0",jobset_name="train-jobset",nodepool_name="tpu-pool-1",subblock_id="6ce4a464bd524e332477fad57c0875a5",subblock_name="tpu-block-0001-subblock-0002"} 1

megamon_slice_up{block_id="9a0e671424e45fd480ca172ad7a4e25d",block_name="tpu-block-0001",slice_name="js-slice-0",slice_owner_kind="JobSet",slice_owner_name="train-jobset",slice_owner_namespace="default",tpu_accelerator="tpu-v5p",tpu_topology="2x2x2"} 1
```

## v1.1.1

### New features and improvements
* Add standardized `<resource>.provisioning.duration` metric family (e.g. `nodepool.provisioning.duration`, `jobset.provisioning.duration`) to track resource provisioning latency with state tracking (`provisioning`, `success`, `failed`). See [docs/metrics.md](metrics.md) for details.

## v1.1.0

### New features and improvements
* Initial support for Slice metrics (set `SliceEnabled` to true)
    * Support for MTBI and MTTR for Slice
    * Support for tracking slice provisioning time
    * When Megamon operates with "SliceEnabled"
        * `jobset_node` metrics are not emitted
        * `tpu_topology` labels are not populated on jobset metrics
          * instead use new slice metrics
    * **NB:** Do not enable if the Slice CRD is not installed on the cluster
* Add `tpu_accelerator` label to nodepool metrics
* Improved Time Between Interruption (TBI) accuracy by distinguishing between JobSet Completed (expected) and Failed/Suspended (interruption) terminal states in TBI calculation
* New `megamon.build.info` metric to track deployment versions 

### Fixes
* Eliminate false positive nodepool interruptions by ignoring the nodepool STOPPING state in TBI calculation
* Fixed a bug that caused up/down time duration metrics (e.g., `nodepool_up_time_seconds`) to report small negative values by enforcing timestamp consistency during event processing

### Example of new slice metrics
```
megamon_slice_up{otel_scope_name="megamon",otel_scope_version="",slice_name="js-t4x4x4r2-wg2fp-718eee99-myjob-0",slice_owner_kind="jobset",slice_owner_name="t4x4x4r2-wg2fp",slice_owner_namespace="default"
,tpu_accelerator="tpu7x",tpu_topology="4x4x4"} 1
megamon_slice_up{otel_scope_name="megamon",otel_scope_version="",slice_name="js-t4x4x4r2-wg2fp-718eee99-myjob-1",slice_owner_kind="jobset",slice_owner_name="t4x4x4r2-wg2fp",slice_owner_namespace="default"
,tpu_accelerator="tpu7x",tpu_topology="4x4x4"} 1
```
 * "up" metric for jobset `t4x4x4r2-wg2fp`
 * jobset may have 1 or more slices, this example has 2

### Misc
 * New file docs/arch.md generated with Gemini to describe MegaMon architecture
 * Adopt reconciler for Jobset metrics vs polling
 * Introduce new ConfigMap, SliceOwnerMap to track what resource "owns" a slice
 * Polling and reporting interval can be indepedent of each other
 * Megamon will not emit metrics until poller and reconciler have run at least once
