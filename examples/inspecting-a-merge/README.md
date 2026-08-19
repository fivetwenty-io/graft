# Inspecting a Merge

Fixtures for the [Inspecting a Merge](../../docs/examples/inspecting-a-merge.md)
walkthrough, which covers `graft debug` and `graft diff` by working
through a merge that fails with an unhelpful error.

| File | Role |
|------|------|
| `base.yml` | Product defaults, and every operator the walkthrough uses |
| `env-prod.yml` | What production changes about the defaults |
| `env-staging.yml` | What staging changes, so the two can be compared |
| `sizing.yml` | Capacity decisions, layered last |

Start here:

```sh
cd examples/inspecting-a-merge
graft merge base.yml env-prod.yml sizing.yml
```

That merge fails on purpose. `base.yml` reads a secret from Vault, which
you are not expected to have, and the walkthrough is about finding out
what else is true despite that failure.
