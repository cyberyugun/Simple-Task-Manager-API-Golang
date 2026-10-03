# Regional DR Game Day Checklist

Use this checklist during a controlled regional recovery exercise.

## Preparation

- [ ] incident/game-day owner assigned
- [ ] recovery region selected
- [ ] target RPO timestamp recorded
- [ ] start time recorded
- [ ] latest signed application digest recorded
- [ ] Terraform state backend accessible
- [ ] DNS/traffic owner available
- [ ] source production environment protected from accidental destructive commands

## Data recovery

- [ ] PostgreSQL recovery started
- [ ] recovered database is a new/isolated target
- [ ] schema migration versions verified
- [ ] application-level row/data checks completed
- [ ] measured database RPO recorded
- [ ] Redis restored or intentionally recreated

## Infrastructure recovery

- [ ] network provisioned
- [ ] Kubernetes control plane available
- [ ] at least two failure domains available
- [ ] platform add-ons installed
- [ ] secrets restored from external secret source
- [ ] cert-manager/TLS operational
- [ ] monitoring stack operational

## Application recovery

- [ ] immutable image signature verified
- [ ] migration compatibility approved
- [ ] deployment rollout successful
- [ ] at least three API replicas ready
- [ ] replicas distributed across failure domains
- [ ] health endpoint passes
- [ ] readiness endpoint passes
- [ ] version endpoint reports expected commit
- [ ] login/authentication smoke test passes
- [ ] task create/read/update smoke test passes

## Traffic recovery

- [ ] public certificate valid
- [ ] synthetic probe passes
- [ ] DNS/traffic cutover approved
- [ ] traffic switched
- [ ] 5xx ratio normal
- [ ] p95 latency normal
- [ ] PostgreSQL/Redis metrics normal

## Closure

- [ ] recovery end time recorded
- [ ] measured RTO recorded
- [ ] measured RPO recorded
- [ ] gaps and manual steps documented
- [ ] corrective actions assigned
- [ ] temporary recovery resources tracked for cleanup
- [ ] game-day report stored
