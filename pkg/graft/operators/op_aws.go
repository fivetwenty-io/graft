package operators

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	awsSDK "github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/goccy/go-yaml"

	awsbackend "github.com/fivetwenty-io/graft/internal/backends/aws"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// Helper function wrappers that delegate to the aws backend package.
// These are kept as package-level functions so other operators (e.g. NATS)
// that shared these helpers continue to compile.

func parseDurationOrDefault(value string, defaultValue time.Duration) time.Duration {
	return awsbackend.ParseDurationOrDefault(value, defaultValue)
}

func parseIntOrDefault(value string, defaultValue int) int {
	return awsbackend.ParseIntOrDefault(value, defaultValue)
}

func parseBoolOrDefault(value string) bool {
	return awsbackend.ParseBoolOrDefault(value)
}

// AwsOperator provides two operators;  (( awsparam "path" )) and (( awssecret "name_or_arn" ))
// It will fetch parameters / secrets from the respective AWS service.
//
// AwsOperator supports the `@target` operator-call syntax (e.g.
// `(( awsparam@myaccount "path" ))`): Opcall.Run sets Evaluator.Target from
// the parsed Expr's target before calling Run, and Run selects a
// target-specific session from internal/backends/aws.ClientPool
// (AWS_<TARGET>_REGION, etc.) when it is non-empty, falling back to the
// existing plain-environment session otherwise.
type AwsOperator struct {
	variant string
}

// SupportsTarget reports that awsparam/awssecret honor "@target" (spec
// cluster A7 §7).
func (AwsOperator) SupportsTarget() bool {
	return true
}

// Setup ...
func (AwsOperator) Setup() error {
	return nil
}

// Phase ...
func (AwsOperator) Phase() OperatorPhase {
	return EvalPhase
}

// Dependencies is not used by AwsOperator.
func (AwsOperator) Dependencies(_ *Evaluator, _ []*Expr, _ []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor {
	return auto
}

// Run will invoke the appropriate getAws* function for each instance of the AwsOperator
// and extract the specified key (if provided).
//
//nolint:gocyclo // AWS operator handles multiple secret types and argument formats
func (o AwsOperator) Run(ev *Evaluator, args []*Expr) (*Response, error) {
	var err error
	DEBUG("running (( %s ... )) operation at $.%s", o.variant, ev.Here)
	defer DEBUG("done with (( %s ... )) operation at $.%s\n", o.variant, ev.Here)

	if len(args) < 1 {
		return nil, fmt.Errorf("%s operator requires at least one argument", o.variant)
	}

	var l []string
	for i, arg := range args {
		// Use ResolveOperatorArgument to support nested expressions
		val, resolveErr := ResolveOperatorArgument(ev, arg)
		if resolveErr != nil {
			DEBUG("  arg[%d]: failed to resolve expression to a concrete value", i)
			DEBUG("     [%d]: error was: %s", i, resolveErr)
			return nil, resolveErr
		}

		if val == nil {
			DEBUG("  arg[%d]: resolved to nil", i)
			return nil, fmt.Errorf("%s operator argument cannot be nil", o.variant)
		}

		switch v := val.(type) {
		case string:
			DEBUG("  arg[%d]: using string value '%v'", i, v)
			l = append(l, v)

		case int, int64, float64, bool:
			DEBUG("  arg[%d]: converting %T to string", i, v)
			l = append(l, fmt.Sprintf("%v", v))

		case map[string]interface{}:
			DEBUG("  arg[%d]: %v is not a string scalar", i, v)
			return nil, ansi.Errorf("@R{%s operator argument is a map; only scalars are supported here}", o.variant)

		case []interface{}:
			DEBUG("  arg[%d]: %v is not a string scalar", i, v)
			return nil, ansi.Errorf("@R{%s operator argument is a list; only scalars are supported here}", o.variant)

		default:
			DEBUG("  arg[%d]: using value of type %T as string", i, val)
			l = append(l, fmt.Sprintf("%v", val))
		}
	}

	key, params, err := parseAwsOpKey(strings.Join(l, ""))
	if err != nil {
		return nil, err
	}

	DEBUG("     [0]: Using %s key '%s'\n", o.variant, key)

	engine := graft.GetEngine(ev)
	var value string
	if !engine.GetOperatorState().IsAWSSkipped() {
		awsSess, cacheTarget, sessErr := o.resolveSession(ev.Target)
		if sessErr != nil {
			return nil, sessErr
		}

		switch o.variant {
		case "awsparam":
			value, err = o.getAwsParam(awsSess, cacheTarget, key)
		case "awssecret":
			value, err = o.getAwsSecret(awsSess, cacheTarget, key, params)
		}

		if err != nil {
			return nil, fmt.Errorf("$.%s error fetching %s: %w", key, o.variant, err)
		}

		subkey := params.Get("key")
		if subkey != "" {
			tmp := make(map[string]interface{})
			err := yaml.Unmarshal([]byte(value), &tmp)

			if err != nil {
				return nil, fmt.Errorf("$.%s error extracting key: %w", key, err)
			}

			tmp = graft.NormalizeMap(tmp)

			if _, ok := tmp[subkey]; !ok {
				return nil, fmt.Errorf("$.%s invalid key '%s'", key, subkey)
			}

			value = fmt.Sprintf("%v", tmp[subkey])
		}
	} else {
		// When AWS is skipped (including via the REDACT environment variable,
		// see evaluator.go and engine.go), return the literal "REDACTED"
		// without making a backend call, matching spruce's op_aws.go and this
		// package's vault/NATS operators (op_vault.go, op_nats.go).
		value = "REDACTED"
	}

	return &Response{
		Type:  Replace,
		Value: value,
	}, nil
}

// parseAwsOpKey parsed the parameters passed to AwsOperator.
// Primarily it splits the key from the extra arguments (specified as a query string).
func parseAwsOpKey(key string) (string, url.Values, error) {
	split := strings.SplitN(key, "?", 2)
	if len(split) == 1 {
		split = append(split, "")
	}

	values, err := url.ParseQuery(split[1])
	if err != nil {
		return "", values, fmt.Errorf("invalid argument string: %w", err)
	}

	return split[0], values, nil
}

// resolveSession returns the AWS session to use and the pool cache
// namespace it was resolved under. A non-empty target selects a pooled,
// target-specific session and namespaces the cache under the target name,
// erroring if that target has no configuration: unlike
// the no-target path, there is no fallback, since silently falling back to
// the default session is exactly the wrong-account-read risk this wiring
// closes. An empty target keeps the existing behavior verbatim: try the
// "default" pooled session, then fall back to plain AWS_* environment
// variables.
func (o AwsOperator) resolveSession(target string) (*session.Session, string, error) {
	if target != "" {
		awsSess, err := awsbackend.DefaultPool.GetSession(target)
		if err != nil {
			return nil, "", fmt.Errorf("error selecting AWS target %q: %w", target, err)
		}
		return awsSess, target, nil
	}

	awsSess, sessErr := awsbackend.DefaultPool.GetSession("default")
	if sessErr != nil {
		// Fall back to initializing from environment
		awsSess, sessErr = awsbackend.InitializeSession(os.Getenv("AWS_PROFILE"), os.Getenv("AWS_REGION"), os.Getenv("AWS_ROLE"))
		if sessErr != nil {
			return nil, "", fmt.Errorf("error during AWS session initialization: %w", sessErr)
		}
	}
	return awsSess, "default", nil
}

// awsSecretCacheKey builds the cache/dedup identity for a Secrets Manager
// fetch from the base secret ID and its query qualifiers. Only "stage" and
// "version" change what Secrets Manager returns for a given secret ID (see
// buildAwsSecretInput), so those are the only qualifiers folded into the
// identity: "db?version=1" and "db?version=2" must never share a cache
// entry, but "db?version=1&key=username" and "db?version=1&key=password"
// should, since "key" only selects a field from the already-fetched value
// (see AwsOperator.Run's subkey extraction, which runs after this cache
// lookup) and does not change the request sent to AWS. Reading fields with
// params.Get rather than using the raw query string keeps param order from
// mattering, so "db?version=1&stage=x" and "db?stage=x&version=1" produce
// the same identity. An unqualified secret keeps the exact bare-key
// identity it had before this fix, so existing cache entries for
// unqualified specs are unaffected.
//
// version is folded in only when stage is absent, mirroring
// buildAwsSecretInput's own precedence: stage wins when both are present,
// and buildAwsSecretInput never looks at version in that case, so
// "db?stage=x&version=1" and "db?stage=x&version=2" send the identical
// request to Secrets Manager and must share one cache entry, not fragment
// into two independent fetches for what is really the same request.
func awsSecretCacheKey(secret string, params url.Values) string {
	stage := params.Get("stage")
	version := params.Get("version")
	if stage == "" {
		if version == "" {
			return secret
		}
		return secret + "\x00version=" + version
	}
	return secret + "\x00stage=" + stage
}

// buildAwsSecretInput builds the Secrets Manager GetSecretValueInput for a
// getAwsSecret fetch, applying the same stage/version precedence
// (stage wins when both are present) that awsSecretCacheKey's identity
// covers.
func buildAwsSecretInput(secret string, params url.Values) *secretsmanager.GetSecretValueInput {
	input := &secretsmanager.GetSecretValueInput{
		SecretId: awsSDK.String(secret),
	}

	if stage := params.Get("stage"); stage != "" {
		input.VersionStage = awsSDK.String(stage)
	} else if version := params.Get("version"); version != "" {
		input.VersionId = awsSDK.String(version)
	}

	return input
}

// getAwsSecret fetches a secret using the AWS backend cache and session.
// cacheTarget namespaces the secret cache (see resolveSession).
// GetOrFetchSecret both serves cached values without a network call and
// coalesces concurrent requests for the same (target, cache identity) into
// one backend request, rather than exposing the raw cache map for an
// unsynchronized read as the previous implementation did. The cache
// identity passed to GetOrFetchSecret is computed by awsSecretCacheKey, not
// the bare secret string, so two specs for the same secret ID that request
// different stage/version qualifiers (e.g. "db?version=1" vs
// "db?version=2") get independent cache entries and independent fetches
// instead of colliding on one.
func (o AwsOperator) getAwsSecret(awsSession *session.Session, cacheTarget, secret string, params url.Values) (string, error) {
	return awsbackend.DefaultPool.GetOrFetchSecret(cacheTarget, awsSecretCacheKey(secret, params), func() (string, error) {
		client := secretsmanager.New(awsSession)

		output, err := client.GetSecretValue(buildAwsSecretInput(secret, params))
		if err != nil {
			return "", err
		}

		return awsSDK.StringValue(output.SecretString), nil
	})
}

// getAwsParam fetches a parameter using the AWS backend cache and session.
// cacheTarget namespaces the parameter cache (see resolveSession); see
// getAwsSecret for the GetOrFetchParam cache+dedup behavior.
func (o AwsOperator) getAwsParam(awsSession *session.Session, cacheTarget, param string) (string, error) {
	return awsbackend.DefaultPool.GetOrFetchParam(cacheTarget, param, func() (string, error) {
		client := ssm.New(awsSession)

		input := &ssm.GetParameterInput{
			Name:           awsSDK.String(param),
			WithDecryption: awsSDK.Bool(true),
		}

		output, err := client.GetParameter(input)
		if err != nil {
			return "", err
		}

		return awsSDK.StringValue(output.Parameter.Value), nil
	})
}

// NewAwsParamOperator creates a new AWS Parameter Store operator.
func NewAwsParamOperator() *AwsOperator {
	return &AwsOperator{variant: "awsparam"}
}

// NewAwsSecretOperator creates a new AWS Secrets Manager operator.
func NewAwsSecretOperator() *AwsOperator {
	return &AwsOperator{variant: "awssecret"}
}

//nolint:gochecknoinits // Operator registration must happen at package load time
func init() {
	// Register the two variants of the AwsOperator
	RegisterOp("awsparam", AwsOperator{variant: "awsparam"})
	RegisterOp("awssecret", AwsOperator{variant: "awssecret"})
}
