# Editing Config with graft yq

Let's start with the question every operator asks before a deploy, which is where each production VM is going to land.

```sh
$ graft yq -m '.instance_groups[] | .name + ": " + (.networks[0].static_ips | join(", "))' base.yml envs/prod.yml
web: 10.20.0.10, 10.20.0.11, 10.20.0.12, 10.20.0.13
db: 10.20.0.20
```

None of those addresses is written down in any file. The production overlay asks for four web nodes on a /24 subnet, the base template carves a static pool out of that subnet with `ips` and gives each instance group its offsets with `static_ips`, and `-m` merges the two files and resolves every operator before the yq expression runs. Stock yq handles the expression itself just as well, but it reads the template as text, so it sees the operators rather than the deployment.

```sh
$ yq '.instance_groups[] | .name + ": " + .networks[0].static_ips' base.yml
web: (( static_ips 0 1 2 3 4 5 ))
db: (( static_ips 10 ))
```

This tutorial assumes we already know yq, and it leaves the language to yq's own documentation, which covers it well. Each section shows something we can only do because yq and graft's operators live in the same binary, and we follow one small deployment from the first command to the last.

## The Deployment

The fixtures live in `examples/yq-tutorial/`, and every command on this page runs from that directory.

```sh
cd examples/yq-tutorial
```

The template, `base.yml`, is a BOSH-style manifest for a service called `checkout`. It declares two `param` values that every environment has to supply, builds the name and the URL with `concat`, and leaves every address to `ips` and `static_ips`. The web group even learns the database's address by grabbing the first static IP of the db group.

```sh
$ cat base.yml
# the shape every checkout environment shares
meta:
  env: (( param "name the environment in meta.env" ))
  cidr: (( param "copy the subnet CIDR from the cloud inventory" ))
  domain: shop.example.com
  web_instances: 2
name: (( concat "checkout-" meta.env ))
instance_groups:
  - name: web
    instances: (( grab meta.web_instances ))
    networks:
      - name: default
        static_ips: (( static_ips 0 1 2 3 4 5 )) # room for six web nodes
    properties:
      url: (( concat "https://" meta.env "." meta.domain ))
      db_host: (( grab instance_groups.db.networks.default.static_ips.0 ))
  - name: db
    instances: 1
    networks:
      - name: default
        static_ips: (( static_ips 10 ))
networks:
  - name: default
    subnets:
      - range: (( grab meta.cidr ))
        gateway: (( ips meta.cidr 1 ))
        static: (( ips meta.cidr 10 20 )) # .10 through .29 belong to us
```

Each environment is a small overlay under `envs/`.

```sh
$ cat envs/staging.yml envs/prod.yml
# staging runs the template defaults
meta:
  env: staging
  cidr: 10.10.0.0/24
# prod gets more web nodes
meta:
  env: prod
  cidr: 10.20.0.0/24
  web_instances: 4
```

## Growing the Template in Place

Let's say checkout needs a worker group shaped like the web group. Because `graft yq -i` edits a file exactly the way yq does, we can clone the web group on a copy of the template, and its `(( ))` operators come along as ordinary strings, ready for the next merge.

```sh
$ cp base.yml next.yml

$ graft yq -i '.instance_groups += [.instance_groups[0] | .name = "worker" | del(.properties)]' next.yml
```

The edit is sound as YAML, and stock yq would make the same one. The trouble is that the clone kept the web group's `static_ips` offsets, and only a merge can see that. When we ask `-m` for anything at all, graft resolves the template and refuses to give two groups the same address.

```sh
$ graft yq -m '.name' next.yml envs/prod.yml
1 error(s) detected:
 - $.instance_groups.worker.networks.default.static_ips: tried to use IP '10.20.0.10', but that address is already allocated to web/0
```

We fix the offsets with a second surgical edit. The clone also carried the web group's line comment, so we rewrite that in the same pass.

```sh
$ graft yq -i 'with(.instance_groups[2].networks[0].static_ips; . = "(( static_ips 11 12 13 14 15 16 ))" | . line_comment = "workers sit above the db")' next.yml

$ diff base.yml next.yml
21a22,26
>   - name: worker
>     instances: (( grab meta.web_instances ))
>     networks:
>       - name: default
>         static_ips: (( static_ips 11 12 13 14 15 16 )) # workers sit above the db
```

The diff is five added lines and nothing else. The comments, the key order, and every operator in the rest of the template are just as we left them, which is what a reviewer wants to read. The merged view now shows where the workers land. They kept the web group's `grab` of `meta.web_instances`, so production runs four of them.

```sh
$ graft yq -m '.instance_groups[] | .name + ": " + (.networks[0].static_ips | join(", "))' next.yml envs/prod.yml
web: 10.20.0.10, 10.20.0.11, 10.20.0.12, 10.20.0.13
db: 10.20.0.20
worker: 10.20.0.21, 10.20.0.22, 10.20.0.23, 10.20.0.24

$ rm next.yml
```

## Onboarding an Environment from the Cloud Inventory

A new production environment called `eu-prod` is coming online, and its subnet already exists in our cloud account. Before we write anything, let's ask the template what an environment has to supply. A merge of `base.yml` on its own stops at each `param` and prints its message, which gives us a ready-made checklist.

```sh
$ graft yq -m . base.yml
2 error(s) detected:
 - $.meta.cidr: copy the subnet CIDR from the cloud inventory
 - $.meta.env: name the environment in meta.env
```

The answers are in the JSON that our cloud API returns when we list subnets.

```sh
$ cat inventory.json
{
  "vpc": "vpc-7f3e21",
  "subnets": [
    {"id": "subnet-0a11", "cidr": "10.10.0.0/24", "tags": {"app": "checkout", "env": "staging"}},
    {"id": "subnet-0b22", "cidr": "10.20.0.0/24", "tags": {"app": "checkout", "env": "prod"}},
    {"id": "subnet-0c33", "cidr": "10.40.0.0/24", "tags": {"app": "checkout", "env": "eu-prod"}},
    {"id": "subnet-0d44", "cidr": "10.90.0.0/24", "tags": {"app": "search", "env": "prod"}}
  ]
}
```

The inventory looks nothing like an overlay, so we let yq act as the adapter. We pick the checkout subnet tagged `eu-prod`, build the two `meta` keys the checklist asked for, and add a comment that records where the file came from. Since the input is JSON, we pass `-o yaml` to get YAML out.

```sh
$ graft yq -o yaml '.subnets[] | select(.tags.app == "checkout" and .tags.env == "eu-prod") | {"meta": {"env": .tags.env, "cidr": .cidr}} | . head_comment = "generated from inventory.json"' inventory.json > envs/eu-prod.yml

$ cat envs/eu-prod.yml
# generated from inventory.json
meta:
  env: eu-prod
  cidr: 10.40.0.0/24
```

The merge does the rest. The new environment gets a name, a URL, a database address, and web addresses from its own subnet, and we never typed an IP.

```sh
$ graft yq -m '.name, .instance_groups[0].properties, .instance_groups[0].networks[0].static_ips' base.yml envs/eu-prod.yml
checkout-eu-prod
db_host: 10.40.0.20
url: https://eu-prod.shop.example.com
- 10.40.0.10
- 10.40.0.11
```

We keep `envs/eu-prod.yml` around for the next section.

## Gating Every Environment in CI

Our platform has a rule that every production environment runs at least three web nodes. The rule is about the deployment rather than any one file, and `envs/eu-prod.yml` never mentions web nodes at all, so no check on the overlay by itself could catch a violation. A loop over the merged views can. With `-e`, `graft yq` exits 1 when the expression comes out false, and a merge that fails, whether from a missing `param` or a clashing address, exits 1 as well.

```sh
$ for f in envs/*.yml; do graft yq -m -e '(.meta.env | test("prod$") | not) or (.instance_groups[0].instances >= 3)' base.yml "$f" > /dev/null 2>&1 && echo "pass $f" || echo "FAIL $f"; done
FAIL envs/eu-prod.yml
pass envs/prod.yml
pass envs/staging.yml
```

The new environment inherited the template's default of two web nodes. One more in-place edit fixes that, and the comment that yq wrote at the top of the file stays where it was.

```sh
$ graft yq -i '.meta.web_instances = 3' envs/eu-prod.yml

$ cat envs/eu-prod.yml
# generated from inventory.json
meta:
  env: eu-prod
  cidr: 10.40.0.0/24
  web_instances: 3

$ graft yq -m -e '(.meta.env | test("prod$") | not) or (.instance_groups[0].instances >= 3)' base.yml envs/eu-prod.yml
true

$ rm envs/eu-prod.yml
```

## Reviewing a Change Before It Ships

A one-line change to an overlay can move a lot in the deployment, and a reviewer should get to see all of it. Let's scale production to six web nodes on a copy of its overlay.

```sh
$ cp envs/prod.yml prod-next.yml

$ graft yq -i '.meta.web_instances = 6' prod-next.yml

$ diff envs/prod.yml prod-next.yml
5c5
<   web_instances: 4
---
>   web_instances: 6
```

That one line is all a code review would show. `graft diff` compares what each version actually deploys, so we hand it the two merged views through process substitution.

```sh
$ graft diff <(graft yq -m . base.yml envs/prod.yml) <(graft yq -m . base.yml prod-next.yml)

instance_groups.web.instances
± value change
- 4
+ 6

instance_groups.web.networks.default.static_ips
+ two list entries added:
- "10.20.0.14"
- "10.20.0.15"

meta.web_instances
± value change
- 4
+ 6
```

Now the reviewer sees the new instance count and the two addresses that `static_ips` handed out, with no noise from the parts that didn't change. If a later edit pushes past what the template reserved, the same merge refuses before anything ships, because `base.yml` gave the web group only six offsets.

```sh
$ graft yq -i '.meta.web_instances = 7' prod-next.yml

$ graft yq -m . base.yml prod-next.yml
1 error(s) detected:
 - $.instance_groups.web.networks.default.static_ips: not enough static IPs requested for job of 7 instances (only asked for 6)

$ rm prod-next.yml
```

## Where to Go Next

- The [yq reference](../user-guide/yq.md) covers every flag, including `-E` to resolve each file on its own, `--prune` and `--cherry-pick` to trim a merged view, `--sort-keys=spruce` to order keys the way `graft merge` does, exact decimal arithmetic, and the full list of places where `graft yq` departs from yq 4.54.1.

- The [operator guide](../user-guide/operators/index.md) describes `param`, `grab`, `concat`, `ips`, `static_ips`, and every other operator a template can hand to `-m`.

- The [jq guide](../user-guide/jq.md) shows the same `-m` flag on `graft jq`, and `graft yq -m -o json` feeds a merged view to any tool that reads JSON.

- The [aliases guide](../user-guide/aliases.md) explains how `graft install-aliases` lets a script that already calls `yq` pick up `-m` without any other change.
