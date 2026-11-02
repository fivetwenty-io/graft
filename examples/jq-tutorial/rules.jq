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
