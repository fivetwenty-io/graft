# Monthly cost of each instance group, and of the whole deployment.
.pricing as $p
| [.instance_groups[] | {(.name): (.instances * $p.hourly[.vm_type] * $p.hours_per_month)}]
| add
| .total = add
