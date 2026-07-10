package services

import (
	"fmt"
	"os"
	"reflect"
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

// ExpandEnvVars expands environment variable references of any case, in either
// the ${VAR} or bare $VAR form, using os.Expand semantics. Unset variables
// expand to the empty string. A '$' that is not followed by a name (e.g. a
// trailing '$' or '$' before whitespace) is left as a literal dollar sign; a
// '$' followed by a name is always treated as a reference, so string values
// such as secrets that contain "$name" sequences are substituted.
func ExpandEnvVars(s string) string {
	return os.Expand(s, os.Getenv)
}

// bindEnvOverrides registers an AETHER_* environment binding for every leaf
// field of the config struct, walking nested structs via their mapstructure
// tags. Binding is required because viper's AutomaticEnv only consults keys it
// already knows; an unbound nested key absent from the YAML file is never looked
// up. Binding an unset variable is safe: viper returns no value, so the field
// keeps the default from models.DefaultConfig().
func bindEnvOverrides(t reflect.Type, prefix string) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("mapstructure"), ",")
		if name == "" || name == "-" {
			continue
		}
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		fieldType := field.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		// Recurse into nested config structs; everything else (time.Duration,
		// slices, maps, scalars) is a bindable leaf.
		if fieldType.Kind() == reflect.Struct {
			bindEnvOverrides(fieldType, key)
			continue
		}
		_ = viper.BindEnv(key)
	}
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
	// Start from a clean singleton so repeated loads don't accumulate stale
	// config or env bindings from a previous call.
	viper.Reset()
	viper.SetConfigFile(configFile)

	// Enable AETHER_ environment overrides. The key replacer maps nested config
	// keys to env names, e.g. services.torch.base_url -> AETHER_SERVICES_TORCH_BASE_URL.
	viper.SetEnvPrefix("AETHER")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// AutomaticEnv only consults keys viper already knows (keys present in the
	// file plus bound keys), so a nested key absent from the file would never be
	// looked up. Binding every config key makes all of them overridable via env,
	// including keys omitted from the YAML file. BindEnv relies on the prefix set
	// just above to form the AETHER_* variable name.
	bindEnvOverrides(reflect.TypeOf(models.ProjectConfig{}), "")

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
