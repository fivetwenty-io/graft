// Package main provides the graft command-line interface.
//
// The graft CLI is built as a consumer of the pkg/graft library, providing
// command-line access to all graft functionality. Uses Cobra for CLI parsing.
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
//	Show the semantic differences between two YAML files.
//	  graft diff file1.yml file2.yml
//
// json:
//
//	Convert YAML to JSON.
//	  graft json input.yml
//
// fan:
//
//	Fan out a source document across multiple target documents.
//	  graft fan source.yml targets.yml
//
// vaultinfo:
//
//	List vault references in the given files.
//	  graft vaultinfo config.yml
//
// # Global Flags
//
//	--debug, -D     Enable debug mode
//	--trace, -T     Enable trace mode (very verbose)
//	--version, -v   Display version information
//	--color         Control color output (on/off/auto)
//
// # Environment Variables
//
//	GRAFT_VAULT_ADDR     Vault/OpenBao server address
//	GRAFT_VAULT_TOKEN    Vault/OpenBao authentication token
//	GRAFT_AWS_REGION     Default AWS region
//	GRAFT_NATS_URL       NATS server URL
package main
