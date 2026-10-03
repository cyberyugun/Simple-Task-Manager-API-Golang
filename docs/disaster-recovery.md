# Disaster Recovery, High Availability, and Business Continuity

Phase 18 defines the production recovery posture for the Task Manager API.

The repository now distinguishes two failure classes:

1. **Availability-zone or dependency failure inside the primary region** — handled automatically by redundant infrastructure and Kubernetes scheduling.
2. **Full regional disaster** — handled by rebuilding infrastructure from Terraform and restoring state from provider backups/PITR according to this runbook.

The repository does **not** claim active-active multi-region operation. A second continuously running production region is not provisioned by default.

## Recovery objectives

Initial objectives:

| Component | RPO target | RTO target | Recovery source |
| --- | ---: | ---: | --- |
| Stateless API | 0 | 15 min | signed image + Terraform/Kubernetes manifests |
| Kubernetes platform | 0 configuration loss | 60 min | Terraform + Git repository |
| PostgreSQL | <= 5 min when provider PITR supports it | 60 min | managed PITR / geo backup / snapshot |
| Redis | <= 60 min; cache/rate-limit state may be rebuilt | 30 min | HA replica + provider snapshots where available |
| Terraform state | <= last successful state write | 30 min | versioned remote backend |
| Observability | <= configured storage retention | 60 min | durable Loki/Tempo/Prometheus storage + Terraform |

These are engineering targets, not guarantees from the repository alone. Actual RPO/RTO must be measured during real cloud recovery exercises.

## In-region high availability

### Kubernetes

The API uses:

- three starting replicas
- HPA minimum of three replicas
- PodDisruptionBudget
- rolling deployment with zero planned unavailable replicas
- mandatory zone topology spread
- hostname topology spread
- readiness probes that remove pods when PostgreSQL or Redis is unavailable

The zone constraint uses:

```text
topology.kubernetes.io/zone
whenUnsatisfiable: DoNotSchedule
```

The cluster must therefore expose at least two usable zones for production.

### AWS

The AWS stack uses:

- 2-3 availability zones, default 3
- one public and one private subnet per selected AZ
- one NAT Gateway per AZ
- private subnet egress through the NAT in the same AZ
- EKS managed nodes across private subnets
- RDS Multi-AZ
- 14-day default RDS automated backup retention
- ElastiCache Multi-AZ with automatic failover
- 14-day default Redis snapshot retention

This removes the previous single NAT Gateway failure domain.

### Azure

The Azure stack uses:

- AKS system nodes distributed across at least two zones, default zones 1/2/3
- three default AKS nodes
- PostgreSQL Flexible Server ZoneRedundant HA
- 35-day default PostgreSQL backup retention
- geo-redundant PostgreSQL backups
- Premium Redis with a replica

Region and SKU availability still need to be verified before applying the stack because not every Azure region/SKU supports every availability-zone combination.

### GCP

The GCP stack uses:

- regional GKE
- regional Cloud SQL HA
- Cloud SQL point-in-time recovery
- retained successful backups in addition to PITR logs
- Memorystore Standard HA
- private service networking

## Regional disaster strategy

A regional disaster is recovered as **rebuild + restore**, not automatic active-active failover.

High-level order:

```text
Incident declaration
      |
      v
Choose recovery region
      |
      v
Bootstrap/verify remote Terraform state access
      |
      v
Create/restore cloud data services
      |
      v
Provision Kubernetes + platform add-ons
      |
      v
Restore PostgreSQL
      |
      v
Restore/recreate Redis if needed
      |
      v
Deploy signed immutable application image
      |
      v
Run migrations only after DB compatibility check
      |
      v
Synthetic + functional validation
      |
      v
Switch DNS/traffic
      |
      v
Observe error rate/latency/readiness
```

Do not move public traffic until health, readiness, version, core authentication, and a read/write task flow pass against the recovered environment.

## PostgreSQL recovery

Prefer provider-native recovery for production:

- AWS RDS PITR or snapshot restore
- Azure PostgreSQL Flexible Server PITR/geo restore
- Google Cloud SQL PITR/backup restore

Recovery procedure:

1. determine the latest safe recovery point
2. restore into a **new** database instance when possible
3. keep the damaged/original instance untouched for investigation
4. compare schema migration versions
5. run application smoke tests against the restored database
6. confirm task/user data integrity
7. rotate application connection secrets
8. deploy application against the restored endpoint
9. switch traffic only after validation

The scheduled logical `pg_dump` / `pg_restore` drill remains an additional portability test.

## Redis recovery

Redis is not the system of record for tasks or users.

Expected impact of total Redis loss:

- authentication rate-limit counters reset
- transient cache/session-adjacent operational state may be lost
- PostgreSQL remains authoritative

Recovery priority is therefore lower than PostgreSQL.

When Redis becomes unavailable, API readiness intentionally fails. With `RATE_LIMIT_FAIL_OPEN=true`, an already-running process can fail open for the authentication limiter, but Kubernetes should stop routing new traffic to the unhealthy pod until Redis recovers.

## Terraform state recovery

All production Terraform state backends are versioned.

If the latest state object is corrupted:

1. stop Terraform apply workflows
2. identify the previous known-good backend object version
3. preserve the corrupt version
4. restore/copy the selected previous version
5. run `terraform plan`
6. verify that the plan reflects actual cloud resources before any apply

Never solve state loss by immediately running `terraform apply` from a blank state.

## DNS and traffic failover

Regional DR requires an external traffic/DNS decision layer. This repository does not automatically create a second-region endpoint.

Recommended production behavior:

- keep DNS TTL low enough for the documented RTO
- pre-document who is authorized to change DNS
- validate TLS certificates in the recovery region before cutover
- use health-checked DNS/load-balancer failover if the selected cloud architecture supports it
- do not point traffic at a recovery cluster until data recovery and smoke checks pass

Future active-passive multi-region infrastructure can automate this step without changing the application API.

## Automated dependency failure exercise

`.github/workflows/dr-exercise.yml` runs monthly and can be dispatched manually.

It builds the Docker Compose stack, then exercises:

1. Redis outage
2. expected `/ready = 503`
3. expected `/health = 200`
4. Redis recovery
5. expected readiness recovery
6. PostgreSQL outage
7. expected `/ready = 503`
8. expected `/health = 200`
9. PostgreSQL recovery
10. database query verification

The same resilience script runs during CI so changes that break dependency-recovery behavior are blocked before merge.

## Backup restore verification

The Phase 17 weekly restore workflow remains part of the Phase 18 DR controls.

It must use:

```text
BACKUP_RESTORE_ENABLED=true
BACKUP_DATABASE_URL=<source>
RESTORE_DATABASE_URL=<isolated disposable target>
```

Never set the restore target to production.

A provider snapshot/PITR exercise should additionally be run at least quarterly in the actual cloud account because logical dumps cannot prove provider snapshot recovery.

## DR readiness policy

`scripts/dr-readiness-check.sh` provides a repository-level policy gate. It asserts that:

- AWS uses per-AZ NAT resources
- RDS Multi-AZ is enabled
- AWS Redis automatic failover is enabled
- Azure PostgreSQL uses ZoneRedundant HA and geo backups
- AKS node zones are configured
- Cloud SQL is regional with PITR
- Memorystore uses Standard HA
- API workloads have zone spreading
- HPA minimum replicas is three
- PDB exists

This is a static guard against accidentally removing critical HA controls.

## Exercise cadence

Recommended minimum:

| Exercise | Cadence |
| --- | --- |
| Docker dependency failure/recovery | every PR + monthly scheduled workflow |
| logical PostgreSQL restore | weekly |
| provider PostgreSQL PITR/snapshot restore | quarterly |
| Kubernetes rebuild from Terraform | quarterly |
| complete regional DR game day | twice per year |
| incident contact/runbook review | quarterly |

Record actual recovery times. If a drill exceeds an RTO or loses more data than the RPO permits, open a corrective action before declaring the DR posture healthy.

## Regional DR acceptance criteria

A regional game day is successful only when all of the following are demonstrated:

- infrastructure can be recreated from reviewed Terraform
- PostgreSQL is restored to the selected recovery point
- application migrations match the recovered database
- Redis is recovered or safely recreated
- at least three API replicas become ready
- replicas span at least two failure domains
- `/health`, `/ready`, and `/version` pass
- authentication flow passes
- task create/read/update flow passes
- Prometheus scraping and core SLO alerts work
- traces/logs are visible or an observability exception is documented
- synthetic probe succeeds
- traffic/DNS cutover procedure is demonstrated
- measured RPO and RTO are recorded

## Cost note

Higher availability increases cloud cost. In particular, AWS now defaults to one NAT Gateway per selected AZ and three EKS nodes. Production resilience should be an explicit business decision; for development environments, lower-cost overrides can be supplied through Terraform variables without weakening production defaults.
