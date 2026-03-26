package graft

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCherryPickWithComplexDependencies(t *testing.T) {
	Convey("Cherry-pick with complex dependency chains", t, func() {
		Convey("Should evaluate transitive dependencies", func() {
			// Create a document with complex dependency chains
			doc := NewDocument(map[string]interface{}{
				"base": map[string]interface{}{
					"url": "(( concat config.protocol \"://example.com\" ))",
				},
				"config": map[string]interface{}{
					"protocol": "(( grab env.protocol || \"https\" ))",
					"timeout":  30,
				},
				"env": map[string]interface{}{
					"protocol": "https",
					"debug":    false,
				},
				"api": map[string]interface{}{
					"endpoint": "(( concat base.url \"/api/v1\" ))",
					"auth_url": "(( concat base.url \"/auth\" ))",
				},
				"unused": map[string]interface{}{
					"error": "(( grab nonexistent.value ))", // Should not be evaluated
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick only api section - should pull in base, config, and env
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("api").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			// Check that operators were evaluated correctly
			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			api, ok := data["api"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(api["endpoint"], ShouldEqual, "https://example.com/api/v1")
			So(api["auth_url"], ShouldEqual, "https://example.com/auth")

			// Unused section should not be present
			So(data["unused"], ShouldBeNil)
		})

		Convey("Should handle circular dependencies gracefully", func() {
			// Create a document with potential circular refs
			doc := NewDocument(map[string]interface{}{
				"a": map[string]interface{}{
					"value": "(( grab b.value ))",
				},
				"b": map[string]interface{}{
					"value": "(( grab c.value ))",
				},
				"c": map[string]interface{}{
					"value": "final",
					"ref":   "(( grab a.value ))", // Creates a cycle
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick a - should handle the cycle properly
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("a").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			// Check result
			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			a, ok := data["a"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(a["value"], ShouldEqual, "final")
		})

		Convey("Should handle operators within arrays", func() {
			doc := NewDocument(map[string]interface{}{
				"databases": []interface{}{
					map[string]interface{}{
						"name": "primary",
						"host": "(( grab hosts.primary ))",
						"port": "(( grab defaults.db_port ))",
					},
					map[string]interface{}{
						"name": "secondary",
						"host": "(( grab hosts.secondary ))",
						"port": "(( grab defaults.db_port ))",
					},
				},
				"hosts": map[string]interface{}{
					"primary":   "db1.example.com",
					"secondary": "db2.example.com",
				},
				"defaults": map[string]interface{}{
					"db_port":    5432,
					"cache_port": 6379,
				},
				"unused": map[string]interface{}{
					"data": "(( grab missing ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick databases - should pull in hosts and defaults.db_port
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("databases").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			// Check that array operators were evaluated
			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			databases, ok := data["databases"].([]interface{})
			So(ok, ShouldBeTrue)
			So(len(databases), ShouldEqual, 2)

			primary, ok := databases[0].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(primary["host"], ShouldEqual, "db1.example.com")
			So(primary["port"], ShouldEqual, 5432)

			secondary, ok := databases[1].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(secondary["host"], ShouldEqual, "db2.example.com")
			So(secondary["port"], ShouldEqual, 5432)

			// Unused should not be present
			So(data["unused"], ShouldBeNil)
		})

		Convey("Should handle deeply nested operator dependencies", func() {
			doc := NewDocument(map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": map[string]interface{}{
						"level3": map[string]interface{}{
							"value": "(( grab level4.level5.level6.final ))",
						},
					},
				},
				"level4": map[string]interface{}{
					"level5": map[string]interface{}{
						"level6": map[string]interface{}{
							"final": "(( concat prefix.value suffix.value ))",
						},
					},
				},
				"prefix": map[string]interface{}{
					"value": "start-",
				},
				"suffix": map[string]interface{}{
					"value": "-end",
				},
				"unrelated": map[string]interface{}{
					"error": "(( grab does.not.exist ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick level1 - should pull in entire dependency chain
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("level1").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			// Check deep evaluation
			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			level1, ok := data["level1"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			level2, ok := level1["level2"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			level3, ok := level2["level3"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(level3["value"], ShouldEqual, "start--end")

			// Unrelated should not be present
			So(data["unrelated"], ShouldBeNil)
		})

		Convey("Should handle conditional operators", func() {
			doc := NewDocument(map[string]interface{}{
				"environment": "production",
				"is_prod":     true,
				"features": map[string]interface{}{
					"ssl":      "(( grab is_prod ))",
					"debug":    false,
					"replicas": "(( is_prod ? 3 : 1 ))",
				},
				"config": map[string]interface{}{
					"url": "(( features.ssl ? \"https://api.example.com\" : \"http://api.example.com\" ))",
				},
				"unused": map[string]interface{}{
					"bad": "(( grab nowhere ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick config - should evaluate conditional chain
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("config").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			// Check conditional evaluation
			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			config, ok := data["config"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(config["url"], ShouldEqual, "https://api.example.com")

			// Unused should not be present
			So(data["unused"], ShouldBeNil)
		})

		Convey("Should handle operators with array paths", func() {
			doc := NewDocument(map[string]interface{}{
				"services": []interface{}{
					map[string]interface{}{
						"name": "web",
						"instances": []interface{}{
							map[string]interface{}{
								"id":   "web-1",
								"port": "(( grab defaults.web_port ))",
							},
							map[string]interface{}{
								"id":   "web-2",
								"port": "(( grab services.0.instances.0.port ))", // Reference to sibling
							},
						},
					},
				},
				"defaults": map[string]interface{}{
					"web_port": 8080,
					"api_port": 9090,
				},
				"monitoring": map[string]interface{}{
					"targets": []interface{}{
						"(( grab services.0.instances.0.id ))",
						"(( grab services.0.instances.1.id ))",
					},
				},
				"unused": map[string]interface{}{
					"error": "(( grab fail ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick monitoring - should resolve array path dependencies
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("monitoring").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			// Check array path resolution
			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			monitoring, ok := data["monitoring"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			targets, ok := monitoring["targets"].([]interface{})
			So(ok, ShouldBeTrue)
			So(len(targets), ShouldEqual, 2)
			So(targets[0], ShouldEqual, "web-1")
			So(targets[1], ShouldEqual, "web-2")

			// Unused should not be present
			So(data["unused"], ShouldBeNil)
		})
	})
}

func TestMultipleCherryPickPaths(t *testing.T) {
	Convey("Cherry-pick with multiple paths", t, func() {
		Convey("Should evaluate operators under multiple paths", func() {
			doc := NewDocument(map[string]interface{}{
				"database": map[string]interface{}{
					"host": "(( grab defaults.db_host ))",
					"port": "(( grab defaults.db_port ))",
					"name": "myapp",
				},
				"cache": map[string]interface{}{
					"host": "(( grab defaults.cache_host ))",
					"port": "(( grab defaults.cache_port ))",
					"ttl":  3600,
				},
				"defaults": map[string]interface{}{
					"db_host":    "localhost",
					"db_port":    5432,
					"cache_host": "redis.local",
					"cache_port": 6379,
					"unused":     "value",
				},
				"monitoring": map[string]interface{}{
					"enabled": true,
					"url":     "(( grab invalid.path ))", // Should not be evaluated
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick multiple paths
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("database", "cache").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			// Check that both paths are included and evaluated
			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)

			database, ok := data["database"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(database["host"], ShouldEqual, "localhost")
			So(database["port"], ShouldEqual, 5432)
			So(database["name"], ShouldEqual, "myapp")

			cache, ok := data["cache"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(cache["host"], ShouldEqual, "redis.local")
			So(cache["port"], ShouldEqual, 6379)
			So(cache["ttl"], ShouldEqual, 3600)

			// Monitoring should not be included
			So(data["monitoring"], ShouldBeNil)
			// Note: defaults might be included due to dependencies, but unused field doesn't matter
		})

		Convey("Should handle overlapping dependencies", func() {
			doc := NewDocument(map[string]interface{}{
				"app1": map[string]interface{}{
					"url":     "(( concat shared.protocol \"://\" shared.domain \"/app1\" ))",
					"timeout": "(( grab shared.timeout ))",
				},
				"app2": map[string]interface{}{
					"url":     "(( concat shared.protocol \"://\" shared.domain \"/app2\" ))",
					"retries": "(( grab shared.retries ))",
				},
				"shared": map[string]interface{}{
					"protocol": "https",
					"domain":   "example.com",
					"timeout":  30,
					"retries":  3,
				},
				"other": map[string]interface{}{
					"data": "(( grab missing ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick both apps - should share the same dependencies
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("app1", "app2").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)

			app1, ok := data["app1"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(app1["url"], ShouldEqual, "https://example.com/app1")
			So(app1["timeout"], ShouldEqual, 30)

			app2, ok := data["app2"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(app2["url"], ShouldEqual, "https://example.com/app2")
			So(app2["retries"], ShouldEqual, 3)

			// Other should not be included
			So(data["other"], ShouldBeNil)
		})

		Convey("Should handle nested paths with multiple picks", func() {
			doc := NewDocument(map[string]interface{}{
				"services": map[string]interface{}{
					"web": map[string]interface{}{
						"port":     8080,
						"replicas": "(( grab config.web_replicas ))",
					},
					"api": map[string]interface{}{
						"port":     9090,
						"replicas": "(( grab config.api_replicas ))",
					},
					"worker": map[string]interface{}{
						"replicas": "(( grab config.worker_replicas ))",
					},
				},
				"config": map[string]interface{}{
					"web_replicas":    3,
					"api_replicas":    2,
					"worker_replicas": 5,
				},
				"monitoring": map[string]interface{}{
					"dashboards": map[string]interface{}{
						"web": "(( grab services.web.port ))",
						"api": "(( grab services.api.port ))",
					},
				},
				"unused": map[string]interface{}{
					"error": "(( grab fail ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick nested paths
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("services.web", "services.api", "monitoring.dashboards").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)

			// Check services structure
			services, ok := data["services"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			web, ok := services["web"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(web["port"], ShouldEqual, 8080)
			So(web["replicas"], ShouldEqual, 3)

			api, ok := services["api"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(api["port"], ShouldEqual, 9090)
			So(api["replicas"], ShouldEqual, 2)

			// Worker should not be included
			So(services["worker"], ShouldBeNil)

			// Check monitoring structure
			monitoring, ok := data["monitoring"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			dashboards, ok := monitoring["dashboards"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(dashboards["web"], ShouldEqual, 8080)
			So(dashboards["api"], ShouldEqual, 9090)

			// Unused should not be included
			So(data["unused"], ShouldBeNil)
		})

		Convey("Should handle array paths in multiple picks", func() {
			doc := NewDocument(map[string]interface{}{
				"servers": map[string]interface{}{
					"web": map[string]interface{}{
						"host": "(( concat hosts.prefix \"-web.example.com\" ))",
						"port": 8080,
					},
					"api": map[string]interface{}{
						"host": "(( concat hosts.prefix \"-api.example.com\" ))",
						"port": 9090,
					},
					"db": map[string]interface{}{
						"host": "(( concat hosts.prefix \"-db.example.com\" ))",
						"port": 5432,
					},
				},
				"hosts": map[string]interface{}{
					"prefix": "prod",
				},
				"monitoring": map[string]interface{}{
					"targets": []interface{}{
						"(( grab servers.web.host ))",
						"(( grab servers.api.host ))",
					},
				},
				"unused": map[string]interface{}{
					"data": "(( grab missing ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick specific servers and monitoring
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("servers.web", "servers.api", "monitoring").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)

			// Check servers
			servers, ok := data["servers"].(map[string]interface{})
			So(ok, ShouldBeTrue)

			web, ok := servers["web"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(web["host"], ShouldEqual, "prod-web.example.com")
			So(web["port"], ShouldEqual, 8080)

			api, ok := servers["api"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(api["host"], ShouldEqual, "prod-api.example.com")
			So(api["port"], ShouldEqual, 9090)

			// db should not be included
			So(servers["db"], ShouldBeNil)

			// Check monitoring
			monitoring, ok := data["monitoring"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			targets, ok := monitoring["targets"].([]interface{})
			So(ok, ShouldBeTrue)
			So(len(targets), ShouldEqual, 2)
			So(targets[0], ShouldEqual, "prod-web.example.com")
			So(targets[1], ShouldEqual, "prod-api.example.com")

			// Unused should not be included
			So(data["unused"], ShouldBeNil)
		})

		Convey("Should handle empty cherry-pick list", func() {
			doc := NewDocument(map[string]interface{}{
				"data": map[string]interface{}{
					"value": "(( grab source.value ))",
				},
				"source": map[string]interface{}{
					"value": 42,
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Empty cherry-pick should include everything
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick().
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			// Everything should be evaluated as normal
			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)
			dataMap, ok := data["data"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(dataMap["value"], ShouldEqual, 42)
		})
	})
}

func TestCherryPickWithPruneOperator(t *testing.T) {
	Convey("Cherry-pick interaction with prune operator", t, func() {
		Convey("Should handle prune operator within cherry-picked paths", func() {
			doc := NewDocument(map[string]interface{}{
				"database": map[string]interface{}{
					"host":     "localhost",
					"port":     5432,
					"password": "(( prune ))",
					"username": "admin",
				},
				"cache": map[string]interface{}{
					"host": "redis.local",
					"ttl":  "(( prune ))",
				},
				"unused": map[string]interface{}{
					"secret": "(( prune ))",
					"data":   "value",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick database and cache
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("database", "cache").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			// Check that prune was applied within cherry-picked paths
			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)

			database, ok := data["database"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(database["host"], ShouldEqual, "localhost")
			So(database["port"], ShouldEqual, 5432)
			So(database["username"], ShouldEqual, "admin")
			So(database["password"], ShouldBeNil) // Should be pruned

			cache, ok := data["cache"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(cache["host"], ShouldEqual, "redis.local")
			So(cache["ttl"], ShouldBeNil) // Should be pruned

			// Unused should not be included
			So(data["unused"], ShouldBeNil)
		})

		Convey("Should handle prune references to cherry-picked paths", func() {
			doc := NewDocument(map[string]interface{}{
				"config": map[string]interface{}{
					"api_key": "(( grab secrets.api_key ))",
					"url":     "https://api.example.com",
				},
				"secrets": map[string]interface{}{
					"api_key": "secret123",
					"unused":  "(( prune ))",
				},
				"metadata": map[string]interface{}{
					"version":    "1.0",
					"deprecated": "(( prune ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick config only - should pull in secrets.api_key
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("config").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)

			// Config should be evaluated
			config, ok := data["config"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(config["api_key"], ShouldEqual, "secret123")
			So(config["url"], ShouldEqual, "https://api.example.com")

			// Metadata should not be included
			So(data["metadata"], ShouldBeNil)

			// Note: secrets might be partially included due to dependency,
			// but we don't enforce that unused fields are pruned in dependencies
		})

		Convey("Should apply both cherry-pick and explicit prune", func() {
			doc := NewDocument(map[string]interface{}{
				"services": map[string]interface{}{
					"web": map[string]interface{}{
						"port":  8080,
						"debug": true,
					},
					"api": map[string]interface{}{
						"port":  9090,
						"debug": false,
					},
				},
				"monitoring": map[string]interface{}{
					"enabled": true,
					"endpoints": []interface{}{
						"(( grab services.web.port ))",
						"(( grab services.api.port ))",
					},
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick services and monitoring, then prune debug fields
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("services", "monitoring").
				WithPrune("services.web.debug", "services.api.debug").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)

			// Check services
			services, ok := data["services"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			web, ok := services["web"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(web["port"], ShouldEqual, 8080)
			So(web["debug"], ShouldBeNil) // Explicitly pruned

			api, ok := services["api"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(api["port"], ShouldEqual, 9090)
			So(api["debug"], ShouldBeNil) // Explicitly pruned

			// Check monitoring
			monitoring, ok := data["monitoring"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(monitoring["enabled"], ShouldEqual, true)
			endpoints, ok := monitoring["endpoints"].([]interface{})
			So(ok, ShouldBeTrue)
			So(endpoints[0], ShouldEqual, 8080)
			So(endpoints[1], ShouldEqual, 9090)
		})

		Convey("Should handle prune operator in arrays", func() {
			doc := NewDocument(map[string]interface{}{
				"environments": []interface{}{
					map[string]interface{}{
						"name":    "dev",
						"secrets": "(( prune ))",
						"url":     "http://dev.example.com",
					},
					map[string]interface{}{
						"name":    "prod",
						"secrets": "(( prune ))",
						"url":     "https://prod.example.com",
					},
				},
				"deployment": map[string]interface{}{
					"target": "(( grab environments.1.name ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick everything (to test prune within arrays)
			result, err := engine.Merge(context.Background(), doc).
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)

			// Check environments
			environments, ok := data["environments"].([]interface{})
			So(ok, ShouldBeTrue)
			So(len(environments), ShouldEqual, 2)

			dev, ok := environments[0].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(dev["name"], ShouldEqual, "dev")
			So(dev["url"], ShouldEqual, "http://dev.example.com")
			So(dev["secrets"], ShouldBeNil) // Should be pruned

			prod, ok := environments[1].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(prod["name"], ShouldEqual, "prod")
			So(prod["url"], ShouldEqual, "https://prod.example.com")
			So(prod["secrets"], ShouldBeNil) // Should be pruned

			// Check deployment
			deployment, ok := data["deployment"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(deployment["target"], ShouldEqual, "prod")
		})

		Convey("Should handle conditional values with cherry-pick", func() {
			doc := NewDocument(map[string]interface{}{
				"feature_flags": map[string]interface{}{
					"new_ui":     true,
					"debug_mode": false,
				},
				"config": map[string]interface{}{
					"ui_version": "(( feature_flags.new_ui ? \"v2\" : \"v1\" ))",
					"log_level":  "(( feature_flags.debug_mode ? \"debug\" : \"info\" ))",
					"api_url":    "https://api.example.com",
				},
				"admin": map[string]interface{}{
					"debug_panel": "(( prune ))",
					"user":        "admin",
				},
				"unused": map[string]interface{}{
					"data": "(( grab missing ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick config only
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("config").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)

			// Check config
			config, ok := data["config"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(config["ui_version"], ShouldEqual, "v2")
			So(config["log_level"], ShouldEqual, "info")
			So(config["api_url"], ShouldEqual, "https://api.example.com")

			// Admin and unused should not be included
			So(data["admin"], ShouldBeNil)
			So(data["unused"], ShouldBeNil)
		})
	})
}

func TestCherryPickWithDeferOperator(t *testing.T) {
	Convey("Cherry-pick with defer operator", t, func() {
		Convey("Should handle defer operator within cherry-picked paths", func() {
			doc := NewDocument(map[string]interface{}{
				"templates": map[string]interface{}{
					"web_url": "(( defer concat \"https://\" domain.name \"/\" app.path ))",
					"api_url": "(( defer concat \"https://api.\" domain.name ))",
					"db_url":  "(( defer grab database.url || \"postgres://localhost\" ))",
				},
				"app": map[string]interface{}{
					"path":    "myapp",
					"version": "1.0",
				},
				"domain": map[string]interface{}{
					"name": "example.com",
				},
				"unused": map[string]interface{}{
					"error": "(( grab missing ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick templates only
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("templates").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)

			// Check that defer expressions are preserved
			templates, ok := data["templates"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(templates["web_url"], ShouldEqual, "(( concat \"https://\" domain.name \"/\" app.path ))")
			So(templates["api_url"], ShouldEqual, "(( concat \"https://api.\" domain.name ))")
			So(templates["db_url"], ShouldEqual, "(( grab database.url || \"postgres://localhost\" ))")

			// Unused should not be included
			So(data["unused"], ShouldBeNil)
		})

		Convey("Should handle defer with references outside cherry-pick scope", func() {
			doc := NewDocument(map[string]interface{}{
				"config": map[string]interface{}{
					"url_template": "(( defer concat protocol \"://\" server.host \":\" server.port ))",
					"timeout":      30,
				},
				"server": map[string]interface{}{
					"host": "localhost",
					"port": 8080,
				},
				"protocol": "https",
				"other": map[string]interface{}{
					"data": "(( grab fail ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick config only - defer should work even though it references external values
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("config").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)

			// Check config
			config, ok := data["config"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(config["url_template"], ShouldEqual, "(( concat protocol \"://\" server.host \":\" server.port ))")
			So(config["timeout"], ShouldEqual, 30)

			// Other sections should not be included
			So(data["server"], ShouldBeNil)
			So(data["protocol"], ShouldBeNil)
			So(data["other"], ShouldBeNil)
		})

		Convey("Should handle nested defer expressions", func() {
			doc := NewDocument(map[string]interface{}{
				"generators": map[string]interface{}{
					"urls": map[string]interface{}{
						"base": "(( defer concat scheme \"://\" host ))",
						"full": "(( defer concat generators.urls.base \"/\" path ))",
					},
				},
				"scheme": "https",
				"host":   "api.example.com",
				"path":   "v1/users",
				"unused": map[string]interface{}{
					"fail": "(( grab missing.value ))",
				},
			})

			engine, err := NewEngine()
			So(err, ShouldBeNil)

			// Cherry-pick generators only
			result, err := engine.Merge(context.Background(), doc).
				WithCherryPick("generators").
				Execute()

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)

			data, ok := result.RawData().(map[string]interface{})
			So(ok, ShouldBeTrue)

			// Check generators structure
			generators, ok := data["generators"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			urls, ok := generators["urls"].(map[string]interface{})
			So(ok, ShouldBeTrue)
			So(urls["base"], ShouldEqual, "(( concat scheme \"://\" host ))")
			So(urls["full"], ShouldEqual, "(( concat generators.urls.base \"/\" path ))")

			// Unused should not be included
			So(data["unused"], ShouldBeNil)
		})
	})
}
