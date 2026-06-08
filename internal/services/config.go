package services

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"

	"github.com/medizininformatik-initiative/aether/internal/lib"
	"github.com/medizininformatik-initiative/aether/internal/models"
)

// expandEnvVarsHookFunc returns a mapstructure decode hook that expands ${VAR}
// references in every string value before it is decoded into the config struct.
func expandEnvVarsHookFunc() mapstructure.DecodeHookFuncType {
	return func(from reflect.Type, _ reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String {
			return data, nil
		}
		return ExpandEnvVars(data.(string)), nil
	}
}

// stringToDurationHookFunc returns a mapstructure decode hook that parses string
// values destined for time.Duration fields, accepting both ISO 8601 ("PT30M")
// and Go ("30m") formats. Non-string sources are left for the default decoder.
func stringToDurationHookFunc() mapstructure.DecodeHookFuncType {
	durationType := reflect.TypeOf(time.Duration(0))
	return func(from reflect.Type, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String || to != durationType {
			return data, nil
		}
		return lib.ParseDuration(data.(string))
	}
}

// ExpandEnvVars expands environment variables in the format ${VAR} or $VAR
func ExpandEnvVars(s string) string {
	// Match ${VAR} pattern
	re := regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)
	expanded := re.ReplaceAllStringFunc(s, func(match string) string {
		// Extract variable name (remove ${ and })
		varName := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		// Get environment variable value, return empty string if not set
		return os.Getenv(varName)
	})
	return expanded
}

// LoadConfig loads configuration from the given file and merges with CLI flags.
// The config file path is required; auto-discovery of ./aether.yaml or
// ~/.config/aether/aether.yaml is intentionally not performed.
// Priority order (highest to lowest):
//  1. CLI flags (via viper bindings)
//  2. Environment variables
//  3. Configuration file
//  4. Default values
func LoadConfig(configFile string) (*models.ProjectConfig, error) {
	if configFile == "" {
		return nil, fmt.Errorf("config file is required: pass aether.yaml as the first positional argument (e.g. `aether pipeline start aether.yaml crtdl.json`)")
	}
	viper.SetConfigFile(configFile)

	// Enable environment variable override with AETHER_ prefix
	viper.SetEnvPrefix("AETHER")
	viper.AutomaticEnv()

	// viper.Unmarshal only sees keys present in the file/defaults, so an
	// AutomaticEnv override of a key absent from the file would be dropped.
	// Registering jobs_dir's default keeps AETHER_JOBS_DIR honoured regardless.
	viper.SetDefault("jobs_dir", "./jobs")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", configFile, err)
	}

	// Start from the declared defaults and overlay only the keys present in the
	// config file. mapstructure leaves struct fields whose keys are absent from
	// the source untouched, so an omitted field keeps its DefaultConfig value.
	config := models.DefaultConfig()

	// Pipeline steps must be declared explicitly; the default placeholder steps
	// only apply to programmatic DefaultConfig() callers, not to loaded files.
	config.Pipeline.EnabledSteps = nil

	decodeHook := mapstructure.ComposeDecodeHookFunc(
		expandEnvVarsHookFunc(),
		stringToDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
	)
	if err := viper.Unmarshal(&config, viper.DecodeHook(decodeHook)); err != nil {
		return nil, fmt.Errorf("failed to decode configuration: %w", err)
	}

	// Validate the configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Validate jobs directory exists and is writable
	if err := models.ValidateJobsDir(config.JobsDir); err != nil {
		// Try to create it if it doesn't exist
		if os.IsNotExist(err) {
			if createErr := os.MkdirAll(config.JobsDir, 0755); createErr != nil {
				return nil, fmt.Errorf("failed to create jobs directory: %w", createErr)
			}
		} else {
			return nil, err
		}
	}

	return &config, nil
}

// GetConfigFilePath returns the path to the config file that was loaded
func GetConfigFilePath() string {
	return viper.ConfigFileUsed()
}

// SetConfigValue allows runtime override of config values
// Useful for CLI flag overrides
func SetConfigValue(key string, value any) {
	viper.Set(key, value)
}

// BindFlagToConfig binds a CLI flag to a configuration key
// This allows CLI flags to override config file values
func BindFlagToConfig(flagName string, configKey string) error {
	return viper.BindPFlag(configKey, nil) // Will be bound by cobra command
}
