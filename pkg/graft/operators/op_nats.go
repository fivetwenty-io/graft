package operators

import (
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	natsbackend "github.com/fivetwenty-io/graft/internal/backends/nats"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// NatsOperator provides the (( nats "store_type:path" )) operator
// It will fetch values from NATS JetStream KV or Object stores.
//
// NatsOperator does not support the `@target` operator-call syntax (e.g.
// `(( nats@myserver "kv:path" ))`). The parser accepts that syntax and
// records it on the parsed Expr, but pkg/graft's Opcall type (the object
// whose Run method actually executes) has no field to carry it, so no
// operator's Run ever observes a non-empty target. Multi-cluster NATS
// access is still available today via per-target environment variables
// consumed directly by internal/backends/nats.ClientPool, just not by
// parsing a target out of the operator call itself.
type NatsOperator struct{}

// fetchFromKV retrieves a value from a NATS KV store, using the shared TTL cache.
func (n NatsOperator) fetchFromKV(js jetstream.JetStream, storePath string, config *natsbackend.Config) (interface{}, error) {
	startTime := time.Now()
	operationType := "kv"

	// Audit logging
	if config.AuditLogging {
		DEBUG("AUDIT: Accessing KV store: %s", storePath)
	}

	// Check TTL cache first
	cacheKey := fmt.Sprintf("kv:%s", storePath)
	if val, ok := natsbackend.Cache.Get(cacheKey); ok {
		duration := time.Since(startTime)
		natsbackend.GlobalMetrics.RecordOperation(operationType, duration, false, true)
		return val, nil
	}

	result, err := natsbackend.FetchFromKV(js, storePath, config)
	if err != nil {
		return nil, err
	}

	natsbackend.Cache.Set(cacheKey, result, config.CacheTTL)

	return result, nil
}

// fetchFromObject retrieves a value from a NATS Object store, using the shared TTL cache.
func (n NatsOperator) fetchFromObject(js jetstream.JetStream, storePath string, config *natsbackend.Config) (interface{}, error) {
	startTime := time.Now()
	operationType := natsbackend.StoreObj

	// Audit logging
	if config.AuditLogging {
		DEBUG("AUDIT: Accessing Object store: %s", storePath)
	}

	// Check TTL cache first
	cacheKey := fmt.Sprintf("%s:%s", natsbackend.StoreObj, storePath)
	if val, ok := natsbackend.Cache.Get(cacheKey); ok {
		duration := time.Since(startTime)
		natsbackend.GlobalMetrics.RecordOperation(operationType, duration, false, true)
		return val, nil
	}

	result, err := natsbackend.FetchFromObject(js, storePath, config)
	if err != nil {
		return nil, err
	}

	natsbackend.Cache.Set(cacheKey, result, config.CacheTTL)

	return result, nil
}

// parseNatsConfig extracts configuration from arguments.
//
//nolint:gocyclo // NATS config parsing handles many configuration options
func parseNatsConfig(ev *graft.Evaluator, args []*graft.Expr) (*natsbackend.Config, error) {
	// Default URL from environment or fallback to NATS default
	defaultURL := os.Getenv("NATS_URL")
	if defaultURL == "" {
		defaultURL = nats.DefaultURL
	}

	// Parse environment variables for default configuration
	defaultTimeout := parseDurationOrDefault(os.Getenv("NATS_TIMEOUT"), 5*time.Second)
	defaultRetries := parseIntOrDefault(os.Getenv("NATS_RETRIES"), 3)
	defaultRetryInterval := parseDurationOrDefault(os.Getenv("NATS_RETRY_INTERVAL"), 1*time.Second)
	defaultRetryBackoff := parseFloatOrDefault(os.Getenv("NATS_RETRY_BACKOFF"), 2.0)
	defaultMaxRetryInterval := parseDurationOrDefault(os.Getenv("NATS_MAX_RETRY_INTERVAL"), 30*time.Second)
	defaultTLS := parseBoolOrDefault(os.Getenv("NATS_TLS"))
	defaultCacheTTLEnv := parseDurationOrDefault(os.Getenv("NATS_CACHE_TTL"), natsbackend.DefaultCacheTTL)
	defaultStreamingThreshold := parseInt64OrDefault(os.Getenv("NATS_STREAMING_THRESHOLD"), 10*1024*1024)
	defaultAuditLogging := parseBoolOrDefault(os.Getenv("NATS_AUDIT_LOGGING"))

	config := &natsbackend.Config{
		URL:                defaultURL,
		Timeout:            defaultTimeout,
		Retries:            defaultRetries,
		RetryInterval:      defaultRetryInterval,
		RetryBackoff:       defaultRetryBackoff,
		MaxRetryInterval:   defaultMaxRetryInterval,
		TLS:                defaultTLS,
		CertFile:           os.Getenv("NATS_CERT_FILE"),
		KeyFile:            os.Getenv("NATS_KEY_FILE"),
		CAFile:             os.Getenv("NATS_CA_FILE"),
		InsecureSkipVerify: parseBoolOrDefault(os.Getenv("NATS_INSECURE_SKIP_VERIFY")),
		CacheTTL:           defaultCacheTTLEnv,
		StreamingThreshold: defaultStreamingThreshold,
		AuditLogging:       defaultAuditLogging,
	}

	// If we have a second argument, it could be URL string or config map
	if len(args) > 1 {
		val, err := ResolveOperatorArgument(ev, args[1])
		if err != nil {
			return nil, err
		}

		switch v := val.(type) {
		case string:
			// Simple URL string
			config.URL = v
		case map[string]interface{}:
			// Configuration map
			if url, ok := v["url"]; ok {
				if urlStr, ok := url.(string); ok {
					config.URL = urlStr
				}
			}
			if timeout, ok := v["timeout"]; ok {
				if timeoutStr, ok := timeout.(string); ok {
					if d, err := time.ParseDuration(timeoutStr); err == nil {
						config.Timeout = d
					}
				}
			}
			if retries, ok := v["retries"]; ok {
				switch r := retries.(type) {
				case int:
					config.Retries = r
				case float64:
					config.Retries = int(r)
				}
			}
			if tlsVal, ok := v["tls"]; ok {
				if tlsBool, ok := tlsVal.(bool); ok {
					config.TLS = tlsBool
				}
			}
			if cert, ok := v["cert_file"]; ok {
				if certStr, ok := cert.(string); ok {
					config.CertFile = certStr
				}
			}
			if key, ok := v["key_file"]; ok {
				if keyStr, ok := key.(string); ok {
					config.KeyFile = keyStr
				}
			}
			if ca, ok := v["ca_file"]; ok {
				if caStr, ok := ca.(string); ok {
					config.CAFile = caStr
				}
			}
			if insecure, ok := v["insecure_skip_verify"]; ok {
				if insecureBool, ok := insecure.(bool); ok {
					config.InsecureSkipVerify = insecureBool
				}
			}
			if cacheTTL, ok := v["cache_ttl"]; ok {
				if ttlStr, ok := cacheTTL.(string); ok {
					if d, err := time.ParseDuration(ttlStr); err == nil {
						config.CacheTTL = d
					}
				}
			}
			if streamingThreshold, ok := v["streaming_threshold"]; ok {
				switch st := streamingThreshold.(type) {
				case int:
					config.StreamingThreshold = int64(st)
				case int64:
					config.StreamingThreshold = st
				case float64:
					config.StreamingThreshold = int64(st)
				}
			}
			if auditLogging, ok := v["audit_logging"]; ok {
				if auditBool, ok := auditLogging.(bool); ok {
					config.AuditLogging = auditBool
				}
			}
			if retryInterval, ok := v["retry_interval"]; ok {
				if intervalStr, ok := retryInterval.(string); ok {
					if d, err := time.ParseDuration(intervalStr); err == nil {
						config.RetryInterval = d
					}
				}
			}
			if retryBackoff, ok := v["retry_backoff"]; ok {
				switch b := retryBackoff.(type) {
				case float64:
					config.RetryBackoff = b
				case int:
					config.RetryBackoff = float64(b)
				}
			}
			if maxRetryInterval, ok := v["max_retry_interval"]; ok {
				if intervalStr, ok := maxRetryInterval.(string); ok {
					if d, err := time.ParseDuration(intervalStr); err == nil {
						config.MaxRetryInterval = d
					}
				}
			}
		default:
			return nil, fmt.Errorf("second argument must be URL string or configuration map")
		}
	}

	return config, nil
}

// Setup initializes the NATS operator.
func (NatsOperator) Setup() error {
	return nil
}

// Phase returns the phase when this operator runs.
func (NatsOperator) Phase() graft.OperatorPhase {
	return graft.EvalPhase
}

// Dependencies returns the dependencies for this operator.
func (NatsOperator) Dependencies(ev *graft.Evaluator, args []*graft.Expr, locs []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor {
	return auto
}

// Run executes the NATS operator.
func (n NatsOperator) Run(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
	DEBUG("running (( nats ... )) operation at $.%s", ev.Here)
	defer DEBUG("done with (( nats ... )) operation at $%s\n", ev.Here)

	engine := graft.GetEngine(ev)
	if engine.GetOperatorState().IsNATSSkipped() {
		return &graft.Response{
			Type:  graft.Replace,
			Value: "REDACTED",
		}, nil
	}

	// Validate arguments
	if len(args) < 1 {
		return nil, fmt.Errorf("nats operator requires at least one argument")
	}

	// Resolve the path argument
	pathVal, err := ResolveOperatorArgument(ev, args[0])
	if err != nil {
		return nil, err
	}

	path, ok := pathVal.(string)
	if !ok {
		return nil, ansi.Errorf("@R{first argument to nats operator must be a string}")
	}

	// Parse the path to get store type and path
	storeType, storePath, err := natsbackend.ParsePath(path)
	if err != nil {
		return nil, err
	}

	// Parse configuration
	config, err := parseNatsConfig(ev, args)
	if err != nil {
		return nil, err
	}

	pc, err := natsbackend.ConnPool.GetConnection(config)
	if err != nil {
		return nil, err
	}
	defer natsbackend.ConnPool.ReleaseConnection(config)

	// Fetch the value based on store type
	var value interface{}
	switch storeType {
	case natsbackend.StoreKV:
		value, err = n.fetchFromKV(pc.JS, storePath, config)
	case natsbackend.StoreObj:
		value, err = n.fetchFromObject(pc.JS, storePath, config)
	}

	if err != nil {
		return nil, err
	}

	return &graft.Response{
		Type:  graft.Replace,
		Value: value,
	}, nil
}

// ClearNatsCache clears the NATS cache (useful for testing).
func ClearNatsCache() {
	natsbackend.ClearCache()
}

// GetNatsMetrics returns current NATS operator metrics.
func GetNatsMetrics() map[string]interface{} {
	return natsbackend.GetMetrics()
}

// ShutdownNatsOperator gracefully shuts down NATS connections and goroutines.
func ShutdownNatsOperator() {
	natsbackend.Shutdown()
}

// parseInt64OrDefault delegates to the natsbackend package.
func parseInt64OrDefault(value string, defaultValue int64) int64 {
	return natsbackend.ParseInt64OrDefault(value, defaultValue)
}

// parseFloatOrDefault delegates to the natsbackend package.
func parseFloatOrDefault(value string, defaultValue float64) float64 {
	return natsbackend.ParseFloatOrDefault(value, defaultValue)
}

//nolint:gochecknoinits // Operator registration and background cleanup must start at package load time
func init() {
	// Wire up debug logging for the backend
	natsbackend.DebugFunc = func(format string, args ...interface{}) {
		DEBUG(format, args...)
	}

	go natsbackend.ConnPool.CleanupLoop()
	go func() {
		ticker := time.NewTicker(natsbackend.CacheCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				natsbackend.Cache.Cleanup()
			case <-natsbackend.CacheStopCleanup:
				return
			}
		}
	}()
	RegisterOp("nats", NatsOperator{})
}
