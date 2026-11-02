# Querying Config with graft jq

Let's start with the command that shows what this tutorial is about. When we run it from inside `examples/jq-tutorial/`, it writes Prometheus scrape targets for the production deployment of a small service:

```sh
$ graft jq -m '.name as $deployment | [.instance_groups[] | {targets: [.networks[].static_ips[] + ":9100"], labels: {deployment: $deployment, group: .name}}]' manifest.yml prod.yml
[
  {
    "targets": [
      "10.40.1.10:9100",
      "10.40.2.10:9100",
      "10.40.1.11:9100"
    ],
    "labels": {
      "deployment": "orders-prod",
      "group": "api"
    }
  },
  {
    "targets": [
      "10.40.1.13:9100",
      "10.40.2.13:9100"
    ],
    "labels": {
      "deployment": "orders-prod",
      "group": "worker"
    }
  }
]
```

None of those addresses is written in any file. The `-m` flag merged the base manifest with the production overlay, and then graft's operators went to work. `load` read the subnets out of a Terraform output file, `ips` carved a static pool out of each subnet, `static_ips` handed out addresses across two availability zones, and `concat` built the deployment's name. Only after all of that did the jq program run, and it saw the finished manifest. Stock jq can't read the YAML, and it has no idea what `(( static_ips ))` means, so it would need `graft merge` and a format conversion in front of it before it could even begin.

This tutorial assumes we already know jq, so it never explains the language. Each section is a job we do with real configuration, and each one leans on something that only works because jq runs inside graft.

## Before We Begin

We need `graft` v1.43.0 or newer, and the commands below run from the example directory:

```sh
cd examples/jq-tutorial
```

The files describe a BOSH deployment of a service called `orders`, with two instance groups spread across two availability zones.

| File | What it holds |
|------|---------------|
| `manifest.yml` | The base manifest, which computes almost everything with `(( ))` operators |
| `staging.yml`, `prod.yml` | The environment overlays |
| `terraform-staging.json`, `terraform-prod.json` | Terraform's output for each environment's network |
| `pricing.yml` | List prices for each VM type |
| `cost.jq`, `rules.jq`, `scale.jq` | jq programs that we use in the sections below |
| `scale-OPS-2417.json`, `scale-OPS-2431.json` | Two requests to scale production |

The base manifest asks for an environment and derives the rest from it. Here is its `meta` block:

```yaml
meta:
  env: (( param "Merge an environment overlay, such as staging.yml or prod.yml" ))
  domain: (( concat meta.env ".example.com" ))
  terraform: (( load (concat "terraform-" meta.env ".json") ))
  subnets: (( grab meta.terraform.subnets.value ))
```

The production overlay names the environment and sizes the two instance groups:

```yaml
meta:
  env: prod

instance_groups:
- name: api
  instances: 3
  vm_type: large
- name: worker
  instances: 2
  vm_type: medium
```

## An Inventory of Every Environment

A single merge sees one environment, so a loop runs one merge for each environment, and a second `graft jq` folds the results into one YAML document:

```sh
$ for env in staging prod; do graft jq -m -c '{(.name): {vpc: .meta.terraform.vpc_id.value, url: .instance_groups[0].jobs[0].properties.external_url, hosts: (.instance_groups | map({(.name): .networks[0].static_ips}) | add)}}' manifest.yml $env.yml; done | graft jq -s -o yaml add
orders-staging:
  vpc: vpc-0c4e7a9f
  url: https://orders.staging.example.com
  hosts:
    api:
      - 10.20.1.10
      - 10.20.2.10
    worker:
      - 10.20.1.13
orders-prod:
  vpc: vpc-07f2b1d6
  url: https://orders.prod.example.com
  hosts:
    api:
      - 10.40.1.10
      - 10.40.2.10
      - 10.40.1.11
    worker:
      - 10.40.1.13
      - 10.40.2.13
```

Every value in that inventory was computed by an operator. The VPC came from the Terraform file that `load` pulled in, the URL came from `concat`, and the hosts came from `static_ips`. The JSON that `load` read stays in the merged document, so one query reaches into Terraform's output and into the values graft computed from it. The `-o yaml` flag is graft's own, and it lets the inventory go straight into a runbook or into another merge.

## A Cost Estimate That Adds Up

Prices don't belong in the manifest, but nothing stops us from merging them in for one query. The price list in `pricing.yml` joins the merge like any other file, and `cost.jq` multiplies it out:

```jq
# Monthly cost of each instance group, and of the whole deployment.
.pricing as $p
| [.instance_groups[] | {(.name): (.instances * $p.hourly[.vm_type] * $p.hours_per_month)}]
| add
| .total = add
```

```sh
$ graft jq -m -f cost.jq manifest.yml prod.yml pricing.yml
{
  "api": 420.48,
  "worker": 124.1,
  "total": 544.58
}
```

Those are the figures a person would get with a pencil, because `graft jq` does decimal arithmetic exactly. The `--float-numbers` flag switches to the doubles that jq computes in, and with it we get exactly what stock jq prints for the same program:

```sh
$ graft jq -m --float-numbers -f cost.jq manifest.yml prod.yml pricing.yml
{
  "api": 420.4800000000001,
  "worker": 124.10000000000001,
  "total": 544.58
}
```

The drift is tiny, but it is the kind that breaks a reconciliation against an invoice or a golden file, and with exact arithmetic we never have to round it away.

## A CI Gate That Knows Why It Failed

Our deploy rules live in `rules.jq`. Each broken rule prints a line, and the last line is `true` only when nothing broke, which is what `-e` turns into an exit code:

```jq
# The rules CI checks after every merge. Each broken rule prints a line,
# and the last line is true only when nothing broke.
[
  (.instance_groups[]
    | select(.instances < (.azs | length))
    | "\(.name) runs \(.instances) instance(s) across \(.azs | length) AZs"),
  (.instance_groups[].jobs[].properties.external_url // empty
    | select(startswith("https://") | not)
    | "\(.) is not served over HTTPS")
]
| .[], length == 0
```

Production passes:

```sh
$ graft jq -m -r -e -f rules.jq manifest.yml prod.yml; echo "exit $?"
true
exit 0
```

Staging leaves the worker at one instance, so one of its two availability zones would sit empty:

```sh
$ graft jq -m -r -e -f rules.jq manifest.yml staging.yml; echo "exit $?"
worker runs 1 instance(s) across 2 AZs
false
exit 1
```

The interesting case is the one where the rules never run. If a pipeline forgets the overlay, the `param` in the base manifest stops the merge with its own message, and `graft jq` exits 2 before the program starts:

```sh
$ graft jq -m -r -e -f rules.jq manifest.yml 2>&1; echo "exit $?"
1 error(s) detected:
 - $.meta.env: Merge an environment overlay, such as staging.yml or prod.yml

exit 2
```

So a CI job can tell three outcomes apart from the exit code alone. A 0 means the configuration is complete and follows the rules, a 1 means it is complete but breaks a rule, and a 2 means it never merged at all.

## An Overlay Written by jq

Scale requests reach us as JSON from a ticketing system:

```json
{
  "ticket": "OPS-2417",
  "env": "prod",
  "reason": "Holiday traffic forecast",
  "scale": { "api": 5, "worker": 3 }
}
```

The program in `scale.jq` turns a request into a graft overlay, and `-o yaml` writes it in the form an overlay takes:

```sh
$ graft jq -o yaml -f scale.jq scale-OPS-2417.json
instance_groups:
  - name: api
    instances: 5
  - name: worker
    instances: 3
```

That overlay says nothing about addresses, and it doesn't need to. When we pipe it into a merge as `-`, which stands for standard input, the `static_ips` operators in the base manifest allocate the new addresses and keep alternating between the two zones:

```sh
$ graft jq -o yaml -f scale.jq scale-OPS-2417.json | graft jq -m -c '.instance_groups[] | {(.name): .networks[0].static_ips}' manifest.yml prod.yml -
{"api":["10.40.1.10","10.40.2.10","10.40.1.11","10.40.2.11","10.40.1.12"]}
{"worker":["10.40.1.13","10.40.2.13","10.40.1.14"]}
```

jq can write the request, but only graft knows how many addresses the manifest set aside for each group. The second ticket asks for eight API instances, and the manifest reserves six, so the merge refuses the plan before anything reaches a director:

```sh
$ graft jq -o yaml -f scale.jq scale-OPS-2431.json | graft jq -m -c '.instance_groups[] | {(.name): .networks[0].static_ips}' manifest.yml prod.yml -
1 error(s) detected:
 - $.instance_groups.api.networks.orders-net.static_ips: not enough static IPs requested for job of 8 instances (only asked for 6)
```

## Reviewing the Change with graft diff

Before we deploy the first request, a reviewer wants to see everything it changes. We merge production twice, once as it stands and once with the overlay jq wrote, and hand both results to `graft diff`:

```sh
$ graft diff <(graft merge manifest.yml prod.yml) <(graft jq -o yaml -f scale.jq scale-OPS-2417.json | graft merge manifest.yml prod.yml -)

instance_groups.api.instances
± value change
- 3
+ 5

instance_groups.api.networks.orders-net.static_ips
+ two list entries added:
- "10.40.2.11"
- "10.40.1.12"

instance_groups.worker.instances
± value change
- 2
+ 3

instance_groups.worker.jobs.orders-worker.properties.api_servers
+ two list entries added:
- "10.40.2.11"
- "10.40.1.12"

instance_groups.worker.networks.orders-net.static_ips
+ one list entry added:
- "10.40.1.14"
```

The request touched two numbers, and the diff shows five changes. The fourth is the one a reviewer would miss by reading the ticket. The worker's `api_servers` property is a `grab` of the API's addresses, so it grew along with them. We diffed the resolved manifests rather than the source files, which is why that ripple shows up at all. And because `graft diff` exits 1 when the two sides differ, the same command can serve as a gate.

## Where to Go Next

The [jq command guide](../user-guide/jq.md) is the reference for `graft jq`. It covers the full table of flags, the exit codes, YAML input, the sort-key modes, `-E` for resolving each document of a multi-document file on its own, `--prune` and `--cherry-pick` for trimming a merge, and every place where `graft jq` deliberately differs from jq 1.8.2.

The [operator overview](../user-guide/operators/index.md) lists every `(( ))` operator, and each of them produces data that a jq program can reshape once `-m` has resolved it. The [external sources](../user-guide/operators/external-sources.md) page covers `load` and `file`, which are the easiest way to bring JSON from other tools into a merge.

The [aliases guide](../user-guide/aliases.md) explains `graft install-aliases`, which lets existing scripts that call `jq` by name pick up YAML input, merging, and exact arithmetic without an edit.
