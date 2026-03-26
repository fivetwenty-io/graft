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
	"github.com/geofffranks/yaml"

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
type AwsOperator struct {
	variant string
}

// extractTarget extracts target name from operator call (placeholder).
func (o AwsOperator) extractTarget(ev *Evaluator, args []*Expr) string {
	// TODO: Extract target from parsed expression when parser supports it
	// For now, return empty string to use default configuration
	return ""
}

// getCacheKey generates a cache key that includes target information.
func (o AwsOperator) getCacheKey(target, variant, key string) string {
	if target == "" {
		return fmt.Sprintf("%s:%s", variant, key)
	}
	return fmt.Sprintf("%s@%s:%s", target, variant, key)
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

	// Extract target information (placeholder for now)
	targetName := o.extractTarget(ev, args)

	engine := graft.GetEngine(ev)
	var value string
	if !engine.GetOperatorState().IsAWSSkipped() {
		if targetName != "" {
			// Use target-aware client pool
			value, err = o.getValueFromTarget(targetName, key, params)
		} else {
			// Use default behavior via backend pool
			awsSess, sessErr := awsbackend.DefaultPool.GetSession("default")
			if sessErr != nil {
				// Fall back to initializing from environment
				awsSess, sessErr = awsbackend.InitializeSession(os.Getenv("AWS_PROFILE"), os.Getenv("AWS_REGION"), os.Getenv("AWS_ROLE"))
				if sessErr != nil {
					return nil, fmt.Errorf("error during AWS session initialization: %w", sessErr)
				}
			}

			switch o.variant {
			case "awsparam":
				value, err = o.getAwsParam(awsSess, key)
			case "awssecret":
				value, err = o.getAwsSecret(awsSess, key, params)
			}
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

			if _, ok := tmp[subkey]; !ok {
				return nil, fmt.Errorf("$.%s invalid key '%s'", key, subkey)
			}

			value = fmt.Sprintf("%v", tmp[subkey])
		}
	} else {
		// Return skip message when AWS is skipped
		if targetName != "" {
			value = fmt.Sprintf("<skipped for %s@%s[%s]>", o.variant, targetName, key)
		} else {
			value = fmt.Sprintf("<skipped for %s[%s]>", o.variant, key)
		}
	}

	return &Response{
		Type:  Replace,
		Value: value,
	}, nil
}

// getValueFromTarget retrieves a value from AWS using target-specific clients.
//
//nolint:gocyclo // handles multiple AWS service types (SSM, Secrets Manager) with caching
func (o AwsOperator) getValueFromTarget(targetName, key string, params url.Values) (string, error) {
	config, err := awsbackend.DefaultPool.GetTargetConfig(targetName)
	if err != nil {
		return "", err
	}

	// Audit logging
	if config.AuditLogging {
		DEBUG("AUDIT: Accessing AWS %s: %s (target: %s)", o.variant, key, targetName)
	}

	// Check cache first with target-aware key
	_ = o.getCacheKey(targetName, o.variant, key)

	switch o.variant {
	case "awsparam":
		cache := awsbackend.DefaultPool.GetParamCache(targetName)
		if val, cached := cache[key]; cached {
			if config.AuditLogging {
				DEBUG("AUDIT: Cache hit for %s parameter: %s (target: %s)", o.variant, key, targetName)
			}
			return val, nil
		}

		// Get Parameter Store client for this target
		client, err := awsbackend.DefaultPool.GetParameterStoreClient(targetName)
		if err != nil {
			return "", err
		}

		input := &ssm.GetParameterInput{
			Name:           awsSDK.String(key),
			WithDecryption: awsSDK.Bool(true),
		}

		output, err := client.GetParameter(input)
		if err != nil {
			if config.AuditLogging {
				DEBUG("AUDIT: Failed to retrieve parameter: %s (target: %s) - %v", key, targetName, err)
			}
			return "", err
		}

		value := awsSDK.StringValue(output.Parameter.Value)
		awsbackend.DefaultPool.SetParamCache(targetName, key, value)

		if config.AuditLogging {
			DEBUG("AUDIT: Successfully retrieved parameter: %s (target: %s)", key, targetName)
		}

		return value, nil

	case "awssecret":
		cache := awsbackend.DefaultPool.GetSecretCache(targetName)
		if val, cached := cache[key]; cached {
			if config.AuditLogging {
				DEBUG("AUDIT: Cache hit for %s secret: %s (target: %s)", o.variant, key, targetName)
			}
			return val, nil
		}

		// Get Secrets Manager client for this target
		client, err := awsbackend.DefaultPool.GetSecretsManagerClient(targetName)
		if err != nil {
			return "", err
		}

		input := &secretsmanager.GetSecretValueInput{
			SecretId: awsSDK.String(key),
		}

		if params.Get("stage") != "" {
			input.VersionStage = awsSDK.String(params.Get("stage"))
		} else if params.Get("version") != "" {
			input.VersionId = awsSDK.String(params.Get("version"))
		}

		output, err := client.GetSecretValue(input)
		if err != nil {
			if config.AuditLogging {
				DEBUG("AUDIT: Failed to retrieve secret: %s (target: %s) - %v", key, targetName, err)
			}
			return "", err
		}

		value := awsSDK.StringValue(output.SecretString)
		awsbackend.DefaultPool.SetSecretCache(targetName, key, value)

		if config.AuditLogging {
			DEBUG("AUDIT: Successfully retrieved secret: %s (target: %s)", key, targetName)
		}

		return value, nil

	default:
		return "", fmt.Errorf("unknown AWS operator variant: %s", o.variant)
	}
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

// getAwsSecret fetches a secret using the AWS backend cache and session.
func (o AwsOperator) getAwsSecret(awsSession *session.Session, secret string, params url.Values) (string, error) {
	cache := awsbackend.DefaultPool.GetSecretCache("default")
	if val, cached := cache[secret]; cached {
		return val, nil
	}

	client := secretsmanager.New(awsSession)

	input := &secretsmanager.GetSecretValueInput{
		SecretId: awsSDK.String(secret),
	}

	if params.Get("stage") != "" {
		input.VersionStage = awsSDK.String(params.Get("stage"))
	} else if params.Get("version") != "" {
		input.VersionId = awsSDK.String(params.Get("version"))
	}

	output, err := client.GetSecretValue(input)
	if err != nil {
		return "", err
	}

	value := awsSDK.StringValue(output.SecretString)
	awsbackend.DefaultPool.SetSecretCache("default", secret, value)
	return value, nil
}

// getAwsParam fetches a parameter using the AWS backend cache and session.
func (o AwsOperator) getAwsParam(awsSession *session.Session, param string) (string, error) {
	cache := awsbackend.DefaultPool.GetParamCache("default")
	if val, cached := cache[param]; cached {
		return val, nil
	}

	client := ssm.New(awsSession)

	input := &ssm.GetParameterInput{
		Name:           awsSDK.String(param),
		WithDecryption: awsSDK.Bool(true),
	}

	output, err := client.GetParameter(input)
	if err != nil {
		return "", err
	}

	value := awsSDK.StringValue(output.Parameter.Value)
	awsbackend.DefaultPool.SetParamCache("default", param, value)
	return value, nil
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
