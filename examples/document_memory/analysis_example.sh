#!/bin/bash
# Document Memory Analysis Example
# This script demonstrates how to use document memory for configuration analysis

echo "=== Document Memory Analysis Example ==="
echo

# Enable memory tracking
export GRAFT_MEMORY_ENABLED=true
export GRAFT_MEMORY_MAX_VERSIONS=1000
export GRAFT_MEMORY_COMPRESS_AFTER="24h"

# Create temporary files for the example
cat > base.yaml << 'EOF'
app:
  name: "myapp"
  version: "1.0.0"
  debug: true

database:
  host: "localhost"
  port: 5432
  name: "app_dev"
  pool_size: 5

cache:
  provider: "redis"
  ttl: 3600

features:
  new_ui: false
  analytics: true
  beta: false
EOF

cat > production.yaml << 'EOF'
app:
  version: "2.0.0"
  debug: false
  environment: "production"

database:
  host: "db.production.internal"
  port: 5432
  pool_size: 50
  ssl_mode: "require"

cache:
  ttl: 86400
  cluster_mode: true

features:
  new_ui: true
  analytics: true
  (( prune ))

monitoring:
  enabled: true
  endpoint: "metrics.internal:9090"
EOF

cat > secrets.yaml << 'EOF'
database:
  password: (( vault "secret/db:password" || "default-password" ))
  
app:
  api_key: (( vault "secret/api:key" || "dev-key" ))
  
computed:
  db_url: (( concat "postgresql://" database.host ":" database.port "/" database.name ))
  app_version: (( concat app.name "-" app.version ))
EOF

echo "1. Performing merge with memory tracking..."
graft merge base.yaml production.yaml secrets.yaml --output final.yaml

echo
echo "2. Analyzing changes by phase..."
echo "   Changes during MERGE phase:"
graft history --phase merge --format json | jq '.[] | {path, old_value, new_value, source}'

echo
echo "   Changes during EVAL phase (operators):"
graft history --phase eval --format json | jq '.[] | {path, old_value, new_value, operator}'

echo
echo "3. Tracking specific paths..."
echo "   History of database.host:"
graft history --path "database.host" --verbose

echo
echo "   All database changes:"
graft history --path "database.*" --format table

echo
echo "4. Finding pruned values..."
graft history --operation prune

echo
echo "5. Identifying value sources..."
echo "   Values from production.yaml:"
graft history --source "production.yaml" --format table

echo
echo "6. Comparing versions..."
echo "   Changes to app.version:"
graft history compare --path "app.version" --all-versions

echo
echo "7. Generating audit report..."
cat > audit_report.sh << 'SCRIPT'
#!/bin/bash
echo "=== Configuration Audit Report ==="
echo "Generated: $(date)"
echo

echo "Summary Statistics:"
graft history stats

echo
echo "Critical Changes (database and security):"
graft history --path "database.*" --path "*.password" --path "*.api_key" --format json | \
  jq -r '.[] | "[\(.timestamp)] \(.path): \(.old_value // "null") → \(.new_value // "null") (\(.source))"'

echo
echo "Configuration Sources:"
graft history sources --format table

echo
echo "Operator Usage:"
graft history --phase eval --format json | \
  jq -r 'group_by(.operator) | .[] | {operator: .[0].operator, count: length}'
SCRIPT

chmod +x audit_report.sh
./audit_report.sh

echo
echo "8. Memory usage statistics..."
graft memory stats

echo
echo "9. Exporting full history..."
graft history export --output history.json --format json

echo
echo "10. Example queries for investigation..."
echo "    Recent changes (last hour):"
graft history --after "1 hour ago"

echo
echo "    Changes by a specific operator:"
graft history --operator concat

echo
echo "    Find all additions (no previous value):"
graft history --format json | jq '.[] | select(.old_value == null) | {path, new_value, source}'

# Cleanup
rm -f base.yaml production.yaml secrets.yaml final.yaml audit_report.sh history.json

echo
echo "=== Analysis Complete ==="