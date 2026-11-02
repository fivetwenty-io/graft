# Turn a scale request into a graft overlay that sets each group's instances.
{instance_groups: [.scale | to_entries[] | {name: .key, instances: .value}]}
