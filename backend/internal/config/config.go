// Package config manages environment variables.
//
// It reads variable from the `.env` file,
// loads them into structured Go types (struct), and
// validates that required values are present so they
// can be reused accross the application runtime.
//
// Responsibilities:
//   - Load environment variables (optionally from a `.env` file).
//   - Map env vars into a structured Go config (structs).
//   - Validate required values so the app fails fast on bad/missing config.
//   - Provide sane defaults for optional config blocks (e.g. observability).
package config

import (
	"os"
	"strings"

	"github.com/go-playground/validator/v10"
	// Side-effect import: triggers godotenv's autoload feature.
	// That means: if a `.env` file exists, it gets loaded into process env
	// *before* your code reads env vars. No explicit call needed.
	_ "github.com/joho/godotenv/autoload"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
)

/*
	`koanf` is a config library. Its job is to read config source
	(e.g `.env`, yaml, json, etc) then unmarshal (i.e. decode from lower-level
	to higher-level obejct structure) into your struct.

	Key idea in this file:
	- Env vars are read using a prefix: BOILERPLATE_
	- Keys are normalized (lowercased, prefix removed)
	- Nested struct fields are mapped via "dot notation" using the "." delimiter
	  e.g. BOILERPLATE_SERVER_PORT -> server.port -> Config.Server.Port
*/

// Config is the root configuration object for the application.
//
// The `koanf:"..."` tags specify where koanf should map values from.
// The `validate:"required"` tags are used by go-playground/validator
// to enforce that the config is present and populated.
//
// Observability is a pointer because it is optional. If not provided,
// we inject defaults at runtime.
type Config struct {
	Primary       Primary              `koanf:"primary" validate:"required"`
	Server        ServerConfig         `koanf:"server" validate:"required"`
	Database      DatabaseConfig       `koanf:"database" validate:"required"`
	Redis         RedisConfig          `koanf:"redis" validate:"required"`
	Auth          AuthConfig           `koanf:"auth" validate:"required"`
	Observability *ObservabilityConfig `koanf:"observability"`
}

// Primary holds top-level information about the runtime environment.
// Usually used to tag logs/traces and switch behavior based on env.
type Primary struct {
	Env string `koanf:"env" validate:"required"`
}

// ServerConfig groups settings for the HTTP server runtime.
//
// NOTE: Timeouts are ints here. In many codebases you'll see time.Duration,
// but this tutorial likely stores seconds or milliseconds in env, then parses.
type ServerConfig struct {
	Port               string   `koanf:"port" validate:"required"`
	ReadTimeout        int      `koanf:"read_timeout" validate:"required"`
	WriteTimeout       int      `koanf:"write_timeout" validate:"required"`
	IdleTimeout        int      `koanf:"idle_timeout" validate:"required"`
	CORSAllowedOrigins []string `koanf:"cors_allowed_origins" validate:"required"`
}

// DatabaseConfig contains PostgreSQL connection parameters and pool tuning.
type DatabaseConfig struct {
	Host            string `koanf:"host" validate:"required"`
	Port            int    `koanf:"port" validate:"required"`
	User            string `koanf:"user" validate:"required"`
	Password        string `koanf:"password" validate:"required"`
	Name            string `koanf:"name" validate:"required"`
	SSLMode         string `koanf:"ssl_mode" validate:"required"`
	MaxOpenConns    int    `koanf:"max_open_conns" validate:"required"`
	MaxIdleConns    int    `koanf:"max_idle_conns" validate:"required"`
	ConnMaxLifetime int    `koanf:"conn_max_lifetime" validate:"required"`
	ConnMaxIdleTime int    `koanf:"conn_max_idle_time" validate:"required"`
}

// RedisConfig contains Redis connection details.
// Address is typically "host:port".
type RedisConfig struct {
	Address string `koanf:"address" validate:"required"`
}

// AuthConfig stores authentication-related secrets.
//
// Warning (because humans love footguns):
// Putting secret keys in env is common, but you still need to protect
// your `.env` file and environment access in deployments.
type AuthConfig struct {
	SecretKey string `koanf:"secret_key" validate:"required"`
}

// loadConfig loads configuration from environment variables, unmarshals it into
// Config structs, validates it, applies defaults, and returns the resulting config.
//
// Behavior summary:
//   - Loads env vars with prefix BOILERPLATE_
//   - Converts env keys into koanf keys using "." nesting
//   - Unmarshals into Config
//   - Validates required config blocks/fields
//   - Sets default observability if missing
//   - Overrides observability service name + environment
//   - Validates observability config as well
//
// NOTE: This function *logs fatally* on many errors. That means it will exit
// the process immediately. It only returns an error on the happy path currently,
// which is… a design choice.
func loadConfig() (*Config, error) {
	// Create a logger that writes in a human-friendly console format to STDERR.
	//
	// - zerolog.New(...) builds a base logger
	// - With().Timestamp() adds a timestamp field to each log entry
	// - Logger() finalizes it
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()

	// Create a new koanf instance.
	// The "." is the key-path delimiter koanf uses to represent nesting.
	// e.g. "server.port" means Config.Server.Port
	k := koanf.New(".")

	// Load environment variables into koanf.
	//
	// env.Provider parameters:
	//   1) prefix: "BOILERPLATE_" means only env vars with this prefix are read
	//   2) delimiter: "." tells koanf how to interpret nested keys
	//   3) key-mapping func: transforms raw env var names into koanf keys
	//
	// The mapping function:
	//   - strings.TrimPrefix(s, "BOILERPLATE_") removes the prefix
	//   - strings.ToLower(...) normalizes to lowercase
	//
	// Example:
	//   BOILERPLATE_DATABASE_HOST -> "database_host" (after trim + lower)
	//
	// BUT WAIT: how do we get "database.host" instead of "database_host"?
	// That depends on how your env vars are named. If you use:
	//   BOILERPLATE_DATABASE.HOST
	// it becomes "database.host".
	//
	// If you use underscores, koanf won't magically convert "_" to "." here.
	// Some boilerplates do that conversion in the mapping function. This one doesn't.
	// So your env key format must match what koanf expects.
	err := k.Load(env.Provider("BOILERPLATE_", ".", func(s string) string {
		return strings.ToLower(strings.TrimPrefix(s, "BOILERPLATE_"))
	}), nil)
	if err != nil {
		// Fatal logs the error and exits the program.
		logger.Fatal().Err(err).Msg("Could not load initial env variables.")
	}

	// mainConfig will hold the decoded configuration.
	mainConfig := &Config{}

	// Unmarshal reads the flat key-value store from koanf and fills mainConfig.
	//
	// The first argument is the key path to unmarshal from.
	// Using "" means "unmarshal everything from the root".
	err = k.Unmarshal("", mainConfig)
	if err != nil {
		logger.Fatal().Err(err).Msg("Could not unmarshal main config.")
	}

	// Create a new validator instance.
	// This validator reads `validate:"required"` tags on struct fields.
	validate := validator.New()

	// Validate the entire config struct recursively.
	//
	// Any missing required field triggers an error.
	// Because many structs have validate:"required", it effectively enforces
	// that those blocks exist and have values.
	err = validate.Struct(mainConfig)
	if err != nil {
		logger.Fatal().Err(err).Msg("Config validation failed.")
	}

	// Set default observability config if not provided
	// If observability config wasn't provided, inject a default.
	// It's a pointer field, so nil means "missing".
	if mainConfig.Observability == nil {
		mainConfig.Observability = DefaultObservabilityConfig()
	}

	// Override service name and environment from primary config.
	// Force service name and environment values regardless of what user set.
	// This ensures tracing/logging sees consistent service naming.
	//
	// - ServiceName is hardcoded to "boilerplate"
	// - Environment is derived from Primary.Env
	mainConfig.Observability.ServiceName = "boilerplate"
	mainConfig.Observability.Environment = mainConfig.Primary.Env

	// Validate observability config using its own validation logic.
	// This is separate from go-playground/validator tags and is likely
	// enforcing constraints like "endpoint must be set", "api key required", etc.
	if err := mainConfig.Observability.Validate(); err != nil {
		logger.Fatal().Err(err).Msg("invalid observability config")
	}

	return mainConfig, nil
}
