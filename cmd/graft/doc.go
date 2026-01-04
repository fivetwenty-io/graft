// Package main provides the graft command-line interface.
//
// The graft CLI is built as a consumer of the pkg/graft library, providing
// command-line access to all graft functionality.
//
// # Commands
//
// merge:
//
//	Merge multiple YAML/JSON files with operator evaluation.
//	  graft merge base.yml overlay.yml
//
// diff:
//
//	Compare YAML/JSON files with rich diff output.
//	  graft diff file1.yml file2.yml
//
// json:
//
//	Convert YAML to JSON or vice versa.
//	  graft json input.yml
//
// fan:
//
//	Generate multiple outputs from templates.
//	  graft fan template.yml --values env/*.yml
//
// debug:
//
//	Start interactive debugging REPL.
//	  graft debug config.yml
//
// # Global Flags
//
//	--output, -o    Output format (yaml, json)
//	--quiet, -q     Suppress non-essential output
//	--verbose, -v   Enable verbose output
//	--debug         Enable debug mode
//
// # Environment Variables
//
//	GRAFT_VAULT_ADDR     Vault/OpenBao server address
//	GRAFT_VAULT_TOKEN    Vault/OpenBao authentication token
//	GRAFT_AWS_REGION     Default AWS region
//	GRAFT_NATS_URL       NATS server URL
package main
