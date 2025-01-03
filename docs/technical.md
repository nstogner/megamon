# Technical

## Getting Started

### Prerequisites
- go version v1.22.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster

```bash
gcloud iam service-accounts create megamon

# Allow Megamon to list GKE Node Pools.
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:megamon@${PROJECT_ID}.iam.gserviceaccount.com" \
  --role="roles/container.viewer"

# Create a bucket for megamon to store event history.
gcloud storage buckets create gs://${PROJECT_ID}-megamon
gcloud storage buckets add-iam-policy-binding gs://${PROJECT_ID}-megamon \
  --member="serviceAccount:megamon@${PROJECT_ID}.iam.gserviceaccount.com" \
  --role=roles/storage.objectUser

# Allow Megamon to push OTEL metrics.
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:megamon@${PROJECT_ID}.iam.gserviceaccount.com" \
  --role="roles/monitoring.metricWriter"

# Bind Kubernetes Service Account to GCP Service Account.
gcloud iam service-accounts add-iam-policy-binding megamon@${PROJECT_ID}.iam.gserviceaccount.com \
    --role roles/iam.workloadIdentityUser \
    --member "serviceAccount:${PROJECT_ID}.svc.id.goog[megamon-system/megamon-controller-manager]"
```

```bash
# First edit ./config/dev/service_account_identity.yaml and customize the GCP SA.
make docker-build docker-push IMG=<some-registry>/megamon:tag
make deploy IMG=<some-registry>/megamon:tag
```

### To Uninstall

```bash
make undeploy
```

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)