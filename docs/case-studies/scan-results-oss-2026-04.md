# Scan Results: Major OSS Projects (April 2026)

uzomuzo scan results for 20 major OSS projects. All scans targeted direct dependencies only (go.mod + GitHub Actions via `--include-actions`).

## Scan Conditions

- uzomuzo: main branch build (2026-04-05)
- GITHUB_TOKEN: set (PAT)
- Scope: direct dependencies only (transitive not included)

## go.mod Scan Results

| Project | Stars | Deps | ok | caution | replace (EOL) | review |
|---------|-------|------|----|---------|---------------|--------|
| **CockroachDB** | 30K | 216 | 182 | 17 | **17** | 0 |
| **Grafana** | 65K | 255 | 217 | 25 | **11** | 2 |
| **Vault** | 31K | 209 | 186 | 11 | **11** | 1 |
| **Consul** | 29K | 111 | 97 | 5 | **9** | 0 |
| **Istio** | 37K | 112 | 91 | 17 | **4** | 0 |
| **MinIO** | 50K | 95 | 78 | 12 | **4** | 1 |
| **Terraform** | 44K | 80 | 69 | 8 | **3** | 0 |
| **Moby (Docker)** | 69K | 114 | 96 | 15 | **3** | 0 |
| **ArgoCD** | 18K | 122 | 100 | 19 | **3** | 0 |
| **Trivy** | 24K | 130 | 108 | 20 | **2** | 0 |
| **Prometheus** | 57K | 106 | 94 | 10 | **2** | 0 |
| **Hugo** | 79K | 80 | 65 | 13 | **2** | 0 |
| **vuls** | 12K | 59 | 48 | 9 | **2** | 0 |
| **Gitea** | 46K | 118 | 97 | 19 | **1** | 1 |
| Kubernetes | 114K | 110 | 104 | 6 | 0 | 0 |
| containerd | 18K | 85 | 73 | 12 | 0 | 0 |
| Helm | 27K | 47 | 39 | 8 | 0 | 0 |
| Caddy | 60K | 50 | 44 | 6 | 0 | 0 |
| etcd | 48K | 21 | 19 | 2 | 0 | 0 |
| NATS Server | 16K | 11 | 10 | 1 | 0 | 0 |

## EOL Details for Reported Projects

### Grafana (11 EOL)

```
Azure/go-autorest/autorest@v0.11.30
Azure/go-autorest/autorest/adal@v0.9.24
aws/aws-sdk-go@v1.55.7
benbjohnson/clock@v1.3.5
golang/mock@v1.7.0-rc.1
golang/snappy@v1.0.0
google/wire@v0.7.0
grafana/grafana-api-golang-client@v0.27.0
json-iterator/go@v1.1.12
mitchellh/mapstructure@v1.5.1-0.20231216201459-8508981c8b6c
opentracing/opentracing-go@v1.2.1-0.20220228012449-10b1cf09e00b
```

Additionally, Grafana's GitHub Actions included `tibdex/github-app-token` (archived) in 14 places across 13 workflow files, including release-critical pipelines. Reported as [grafana/grafana#121911](https://github.com/grafana/grafana/issues/121911) — triggered an internal fix within 3 days.

### Vault (11 EOL)

```
Azure/go-autorest/autorest@v0.11.29
Azure/go-autorest/autorest/adal@v0.9.24
aliyun/alibaba-cloud-sdk-go@v1.63.107
aws/aws-sdk-go@v1.55.8
fatih/structs@v1.1.0
google/go-metrics-stackdriver@v0.2.0
hashicorp/hcp-link@v0.2.1
mitchellh/copystructure@v1.2.0
mitchellh/go-homedir@v1.1.0
mitchellh/mapstructure@v1.5.1-0.20231216201459-8508981c8b6c
mitchellh/reflectwalk@v1.0.2
```

Reported as [hashicorp/vault#31899](https://github.com/hashicorp/vault/issues/31899) — focused on 3 mitchellh packages in ACL layer (copystructure, go-homedir, reflectwalk).

### Trivy (2 EOL)

```
mitchellh/go-homedir@v1.1.0
mitchellh/hashstructure/v2@v2.0.2
```

Submitted replacement PR for go-homedir: [aquasecurity/trivy#10484](https://github.com/aquasecurity/trivy/pull/10484) — stdlib replacement with `os.UserHomeDir()`.

## GitHub Actions Scan Results

| Project | Actions | ok | caution | replace (EOL) | review |
|---------|---------|-----|---------|---------------|--------|
| **Consul** | 33 | 26 | 4 | **3** | 0 |
| MinIO | 9 | 8 | 0 | 1 | 0 |
| Grafana | 26 | 25 | 1 | 0 | 0 |
| Vault | 34 | 32 | 1 | 0 | 1 |
| All others | — | — | — | 0 | — |

## Cross-Project Patterns

### mitchellh/* Package Impact

Mitchell Hashimoto archived his personal Go packages in July 2024, affecting the entire Go ecosystem:

| Package | Used by |
|---------|---------|
| mitchellh/go-homedir | vuls, Trivy, Terraform, Vault, MinIO |
| mitchellh/mapstructure | Grafana, Vault, Hugo |
| mitchellh/copystructure | Vault, Consul, Moby, Istio |
| mitchellh/reflectwalk | Vault, Consul, CockroachDB |
| mitchellh/hashstructure | Trivy, Consul |

### Zero-EOL Projects

Kubernetes, containerd, Helm, Caddy, etcd, NATS Server — all had zero EOL dependencies. Kubernetes is notable: 110 direct dependencies with zero EOL.
