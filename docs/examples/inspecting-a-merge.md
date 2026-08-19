# Inspecting a Merge

Graft turns several YAML files into one. When the result is what you
expected, you never need to think about how it got there. When it is not,
you need to see the merge happen rather than guess at it.

This walkthrough starts from a merge that fails with a single unhelpful
line, and ends with you knowing which file set which value, why one
operator resolved the way it did, and how production differs from
staging. Every command runs against files in this repository, and every
transcript on this page is real output from those files.

## The Files

The example lives in `examples/inspecting-a-merge/`. Change into it
first, so the file names in your output match the ones here:

```sh
cd examples/inspecting-a-merge
```

Four files, layered in the order a real deployment usually layers them.

`base.yml` holds the product's defaults and all of its operators:

```yaml
meta:
  environment: (( param "please set meta.environment" ))
  domain: example.com
  replicas: 2
  cpu_per_replica: 2

properties:
  api:
    url: (( concat "https://api." meta.domain ))
    timeout: 30
    admin_password: (( vault "secret/api:admin_password" ))
  totals:
    cpu: (( calc "meta.replicas * meta.cpu_per_replica" ))

instance_groups:
  - name: web
    instances: (( grab meta.replicas ))
    vm_type: small
```

`env-prod.yml` and `env-staging.yml` say what each environment changes,
and `sizing.yml` carries capacity decisions that change on their own
schedule.

## The Problem

Merge them and you get one line back:

```sh
graft merge base.yml env-prod.yml sizing.yml
```

```
1 error(s) detected:
 - $.properties.api.admin_password: Error during Vault client initialization: failed to determine Vault URL / token, and the $REDACT environment variable is not set
```

The message is accurate and almost useless. It tells you which path
failed, but not whether anything else merged correctly, not what the
document looked like when evaluation reached that path, and not which of
the three files is responsible. You cannot even confirm that the rest of
your configuration is sound, because one unreachable secret stopped the
whole run.

The rest of this page replaces that guesswork with evidence.

## Stepping Through the Merge

Open the same files in the debugger:

```sh
graft debug base.yml env-prod.yml sizing.yml
```

`load` parses each file on its own and reports what it contains, before
anything is combined:

```
graft> load
Loaded 3 documents:
  [0] base.yml (3 keys)
  [1] env-prod.yml (2 keys)
  [2] sizing.yml (2 keys)
```

`step` then merges exactly one file and prints every path that file
changed:

```
graft> step
[1/3] Merging env-prod.yml...
  meta.domain: example.com → prod.example.com
  meta.environment: (( param "please set meta.environment" )) → prod
  meta.replicas: 2 → 6
  properties.api.timeout: 30 → 60

graft> step
[2/3] Merging sizing.yml...
  instance_groups.web.vm_type: small → large
  meta.cpu_per_replica: 2 → 4
```

This is already more than the failing merge told you. Both files landed
cleanly, you can see precisely which four values production overrides,
and you can see that `meta.environment` stopped being an unfilled
`param` the moment `env-prod.yml` merged.

Notice what has not happened yet. The operators are still text. Graft
merges structure first and evaluates operators once at the end, and
`step` shows you those as separate events rather than a single result.
That separation is what makes the next problem solvable.

## Getting Past the Blocker

The Vault lookup is the reason the plain merge failed, and you probably
cannot reach Vault from where you are debugging. You do not have to.
`defer` marks a path so evaluation leaves its operator alone. Start a
fresh session, so the whole run is visible, and mark the path before
evaluation reaches it:

```
graft> Loaded 3 documents:
  [0] base.yml (3 keys)
  [1] env-prod.yml (2 keys)
  [2] sizing.yml (2 keys)
graft> Marked properties.api.admin_password for deferred evaluation
graft> [1/3] Merging env-prod.yml...
  meta.domain: example.com → prod.example.com
  meta.environment: (( param "please set meta.environment" )) → prod
  meta.replicas: 2 → 6
  properties.api.timeout: 30 → 60
[2/3] Merging sizing.yml...
  instance_groups.web.vm_type: small → large
  meta.cpu_per_replica: 2 → 4
[3/3] Evaluating operators...
Evaluation complete.
graft> admin_password: (( vault "secret/api:admin_password" ))
timeout: 60
url: https://api.prod.example.com
graft> 
```

Evaluation now completes, and `inspect` shows why that matters.

The one value you cannot reach is still visible as its own unevaluated
expression, and everything around it is fully resolved. You are
debugging a production configuration from a laptop with no credentials.

`defer` rewrites the path's `(( op ... ))` text to `(( defer op ... ))`,
which is the same operator you would write by hand in the source file, so
the behavior matches what a deferred operator does in a normal merge.

### Deferring One Path Versus Redacting Them All

`defer` is precise, which is what you want when a single path is in your
way. When you would rather the whole session tolerate every unreachable
secret at once, set `REDACT` when you launch it:

```sh
REDACT=1 graft debug base.yml env-prod.yml sizing.yml
```

Every Vault lookup then resolves to a placeholder instead of failing:

```
graft> continue
[1/3] Merging env-prod.yml...
[2/3] Merging sizing.yml...
[3/3] Evaluating operators...
Evaluation complete.

graft> inspect properties.api.admin_password
REDACTED
```

Use `defer` to study one operator, and `REDACT` to get an entire session
moving. `REDACT` also makes the output safe to paste into a ticket or a
recording.

## Stopping Where It Matters

On a large document, the change lists from `step` scroll past faster than
you can read them. A breakpoint stops the run at the moment a specific
path changes:

```
graft> break meta.replicas
Breakpoint set on meta.replicas

graft> breaks
Breakpoints:
  - meta.replicas

graft> continue
[1/3] Merging env-prod.yml...
  meta.domain: example.com → prod.example.com
  meta.environment: (( param "please set meta.environment" )) → prod
  meta.replicas: 2 → 6
  properties.api.timeout: 30 → 60
Breakpoint hit: meta.replicas
  Current: 6
```

`continue` runs to the end unless a breakpoint fires, so you can set one
on the value you are suspicious about and let the rest of the merge run
at full speed.

## Answering "Who Set This?"

Breakpoints tell you when a value changed. `history` tells you the whole
story for one path, across every file:

```
graft> defer properties.api.admin_password
Marked properties.api.admin_password for deferred evaluation

graft> history meta.replicas
meta.replicas:
  [0] base.yml       → 2
  [1] env-prod.yml   → 6
  Final              → 6
```

An operator's history looks different, and the difference is the point:

```
graft> history properties.api.url
properties.api.url:
  [0] base.yml       → (( concat "https://api." meta.domain ))
  [3] <evaluated>    → https://api.prod.example.com
  Final              → https://api.prod.example.com
```

`meta.replicas` is a plain value that a later file overrode. The URL was
never touched by any file after the first. It survived every merge as
text and only became a URL in the evaluation phase, which is why it
tracks the domain that production set without production ever mentioning
the URL.

The `defer` on the first line is not decoration. `history` recomputes the
entire merge to trace it, so an operator the session cannot resolve stops
the trace before it starts. Deferring that path first prevents it, and
`REDACT=1` on the session does the same for every Vault path at once.

## Evaluating One Operator On Its Own

`eval` resolves a single operator immediately, wherever the session
happens to be:

```
graft> Loaded 3 documents:
  [0] base.yml (3 keys)
  [1] env-prod.yml (2 keys)
  [2] sizing.yml (2 keys)
graft> Evaluating: (( concat "https://api." meta.domain ))
Result: https://api.example.com
graft> 
```

Run the same command again after `continue` and the answer changes to
`https://api.prod.example.com`. The expression never changed. Its inputs
did. This is the clearest single argument for the debugger over reading
the YAML: an operator's result depends on the merge state at the moment
it runs, and `eval` lets you watch that dependence in action.

## Comparing the Result

Once a merge is working, the question usually becomes how one environment
differs from another. Render both and compare them:

```sh
REDACT=1 graft merge base.yml env-staging.yml sizing.yml > /tmp/staging.yml
REDACT=1 graft merge base.yml env-prod.yml sizing.yml > /tmp/prod.yml
graft diff --changes /tmp/staging.yml /tmp/prod.yml
```

```
Changes (7 modified, 0 added, 0 removed):

  MODIFIED  instance_groups.web.instances
            - 2
            + 6

  MODIFIED  meta.domain
            - staging.example.com
            + prod.example.com

  MODIFIED  meta.environment
            - staging
            + prod

  MODIFIED  meta.replicas
            - 2
            + 6

  MODIFIED  properties.api.timeout
            - 30
            + 60

  MODIFIED  properties.api.url
            - https://api.staging.example.com
            + https://api.prod.example.com

  MODIFIED  properties.totals.cpu
            - 8
            + 24
```

Seven changes, and only four of them were written by anyone. The
environment files declare a domain, an environment name, a replica count,
and a timeout. The remaining three moved on their own because operators
recomputed: `instance_groups.web.instances` follows the replica count,
`properties.api.url` follows the domain, and `properties.totals.cpu` went
from 8 to 24 without appearing in any environment file at all.

`graft diff` renders that same comparison four ways, and each one answers
a different question.

`--changes` gives you the inventory above. It groups by modified,
added, and removed, so you can count what happened before you read it.

For the same change with its surroundings intact, ask for `--unified`:

```sh
graft diff --unified /tmp/staging.yml /tmp/prod.yml
```

```diff
--- /tmp/staging.yml
+++ /tmp/prod.yml
@@ instance_groups @@
-  - instances: 2
+  - instances: 6
     name: web
     vm_type: large
@@ meta @@
   cpu_per_replica: 4
-  domain: staging.example.com
-  environment: staging
-  replicas: 2
+  domain: prod.example.com
+  environment: prod
+  replicas: 6
@@ properties @@
   api:
     admin_password: REDACTED
-    timeout: 30
-    url: https://api.staging.example.com
+    timeout: 60
+    url: https://api.prod.example.com
   totals:
-    cpu: 8
+    cpu: 24
```

Hunks are grouped by top-level key rather than by line number, so the
header tells you which section you are reading. Unchanged keys are left
out entirely.

`--side-by-side` keeps both documents whole and lets you read them as
documents rather than as a change list:

```sh
graft diff --side-by-side /tmp/staging.yml /tmp/prod.yml
```

```
/tmp/staging.yml                       │ /tmp/prod.yml
───────────────────────────────────────┼───────────────────────────────────────
instance_groups:                       │ instance_groups:
- instances: 2                         │ - instances: 6
  name: web                            │   name: web
  vm_type: large                       │   vm_type: large
meta:                                  │ meta:
  cpu_per_replica: 4                   │   cpu_per_replica: 4
  domain: staging.example.com          │   domain: prod.example.com
  environment: staging                 │   environment: prod
  replicas: 2                          │   replicas: 6
properties:                            │ properties:
  api:                                 │   api:
    admin_password: REDACTED           │     admin_password: REDACTED
    timeout: 30                        │     timeout: 60
    url: https://api.staging.example.c │     url: https://api.prod.example.com
  totals:                              │   totals:
    cpu: 8                             │     cpu: 24
```

The staging URL is cut off at the pane boundary, a reminder that the
default total width is 80 columns. Pass `--width 140` when your values
run longer.

Passing no flag at all gives you dyff's own report, the most detailed of
the four. Reach for it when whole subtrees appear or disappear, which is
the case it describes better than the others.

That leaves `--quiet`, for when a script needs the answer instead of the
detail. It prints nothing and exits 1 when the files differ, 0 when they
do not:

```sh
if graft diff --quiet /tmp/staging.yml /tmp/prod.yml; then
  echo "environments match"
else
  echo "environments differ"
fi
```

## A Different Kind of Failure

Not every broken merge is a missing secret. Drop the environment file and
the configuration breaks for a different reason:

```sh
graft debug base.yml sizing.yml
```

```
graft> defer properties.api.admin_password
graft> continue
[1/2] Merging sizing.yml...
[2/2] Evaluating operators...
Evaluation failed: 1 error(s) detected:
 - $.meta.environment: please set meta.environment
```

The `param` operator in `base.yml` exists to make exactly this mistake
loud. It declares that some later file must supply a value, and fails the
merge when none does. Seeing it fire tells you a layer is missing rather
than a value being wrong, which sends you to look for the file you forgot
to pass rather than at the values in the files you did.

## What To Take Away

The failing merge at the top of this page gave you one path and one error
message. Everything after that came out of the same four files.

The habit worth keeping is the order. Graft merges structure first and
evaluates operators afterwards, so almost every confusing result is
really a question about which of those two phases you are looking at.
`step` separates them. `history` tells you which file won a given path,
and marks the values that no file wrote at all. `eval` run before and
after a merge step shows the same expression returning two different
answers, which is usually the moment the merge stops feeling arbitrary.

The blockers are worth keeping too. An operator you cannot resolve is an
inconvenience rather than a wall, because `defer` excuses one path and
`REDACT` excuses the lot. Neither changes what the rest of the document
does.

## See Also

- [debug command reference](../user-guide/cli/debug.md)

- [diff command reference](../user-guide/cli/diff.md)

- [History Tracking](../user-guide/history-tracking.md)

- [Multi-Environment Configurations](multi-environment.md)
