// Package awsfakes provides hand-written test doubles for the aws-sdk-go-v2
// client interfaces internal/backends/aws.SecretsManagerClient,
// internal/backends/aws.SSMClient, and the sts client's GetCallerIdentity
// method (consumed by pkg/graft's unexported mirror interfaces). These are
// plain Go, not counterfeiter-generated: graft has no counterfeiter
// dependency, and the surface here is small enough that hand-writing it is
// simpler than adding a code-generation step.
//
// This package imports only aws-sdk-go-v2 service packages, never
// internal/backends/aws or pkg/graft, so it can be imported from any test
// package in this module (including pkg/graft's own tests) without
// creating an import cycle.
package awsfakes
