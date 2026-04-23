# Kube Resources to Markdown Table

## Execute

Install Go.

```console
go run github.com/syself/kube-resources-md-table@latest
```

The report includes both pods nearing their requests or limits and two low-usage summaries for CPU and memory. Use `--low-threshold` to adjust the default low-usage cutoff of `10%`.

## Start Example output

## reaching cpu-limit

Threshold: `>= 80%`

(none)

## reaching mem-limit

Threshold: `>= 80%`

(none)

## above cpu-request

Threshold: `> 100%`

| namespace   | pod                                    | container            | cpu_use | cpu_request | cpu_req_pct |
| ----------- | -------------------------------------- | -------------------- | ------: | ----------: | ----------: |
| kube-system | metrics-server-5689b7844-bngd2         | metrics-server-nanny |     22m |          5m |      440.0% |
| kube-system | metrics-server-5689b7844-kktln         | metrics-server-nanny |     15m |          5m |      300.0% |
| kube-system | cilium-9dl4f                           | cilium-agent         |    109m |         50m |      218.0% |
| kube-system | cilium-ch2zw                           | cilium-agent         |    102m |         50m |      204.0% |
| kube-system | konnectivity-agent-b968ffdc6-6zmrh     | konnectivity-agent   |     19m |         10m |      190.0% |
| kube-system | cilium-d9tb8                           | cilium-agent         |     93m |         50m |      186.0% |
| kube-system | kube-apiserver-autopilot-1-mngfk-stpt7 | kube-apiserver       |    464m |        250m |      185.6% |
| kube-system | etcd-autopilot-1-mngfk-m7scl           | etcd                 |    170m |        100m |      170.0% |
| kube-system | etcd-autopilot-1-mngfk-stpt7           | etcd                 |    162m |        100m |      162.0% |
| kube-system | cilium-lgwq4                           | cilium-agent         |     76m |         50m |      152.0% |
| kube-system | etcd-autopilot-1-mngfk-w72xs           | etcd                 |    137m |        100m |      137.0% |
| kube-system | cilium-hxsrn                           | cilium-agent         |     62m |         50m |      124.0% |

## above mem-request

Threshold: `> 100%`

| namespace   | pod                                                        | container          | mem_use | mem_request | mem_req_pct |
| ----------- | ---------------------------------------------------------- | ------------------ | ------: | ----------: | ----------: |
| kube-system | etcd-autopilot-1-mngfk-stpt7                               | etcd               |   330Mi |       100Mi |      330.4% |
| kube-system | etcd-autopilot-1-mngfk-w72xs                               | etcd               |   325Mi |       100Mi |      324.8% |
| kube-system | etcd-autopilot-1-mngfk-m7scl                               | etcd               |   310Mi |       100Mi |      310.4% |
| kube-system | cilium-operator-55c45d4cb8-lfxrf                           | cilium-operator    |    72Mi |        25Mi |      289.1% |
| mgt-system  | capi-kubeadm-bootstrap-controller-manager-547b65b848-l8h7l | manager            |   427Mi |       200Mi |      213.3% |
| kube-system | konnectivity-agent-b968ffdc6-dscm2                         | konnectivity-agent |    47Mi |        30Mi |      157.1% |
| kube-system | konnectivity-agent-b968ffdc6-6zmrh                         | konnectivity-agent |    47Mi |        30Mi |      156.7% |
| kube-system | cilium-d9tb8                                               | cilium-agent       |   269Mi |       250Mi |      107.7% |
| kube-system | cilium-ch2zw                                               | cilium-agent       |   263Mi |       250Mi |      105.2% |
| kube-system | cilium-9dl4f                                               | cilium-agent       |   257Mi |       250Mi |      102.6% |
| kube-system | cilium-2pljt                                               | cilium-agent       |   254Mi |       250Mi |      101.8% |
| kube-system | coredns-proportional-autoscaler-75d4c749fd-64cw5           | coredns-autoscaler |   9.6Mi |         10M |      101.0% |
| kube-system | cilium-hxsrn                                               | cilium-agent       |   252Mi |       250Mi |      101.0% |
| kube-system | cilium-operator-55c45d4cb8-6dsld                           | cilium-operator    |    25Mi |        25Mi |      100.4% |

## End Example output
