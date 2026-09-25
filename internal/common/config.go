package common

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

const (
	configFilePath = "/.crusoe/config" // full path is this appended to the user's home path

	defaultApiEndpoint = "https://api.cloud.crusoe.ai/v1"

	// defaultServiceAccountTokenURL is Crusoe's Hydra OAuth2 token endpoint for the
	// client_credentials grant. Overridable via service_account_token_url /
	// CRUSOE_SERVICE_ACCOUNT_TOKEN_URL.
	defaultServiceAccountTokenURL = "https://auth.crusoe.ai/oauth2/token" //nolint:gosec // G101: URL constant, not a credential

	// defaultServiceAccountAudience is the "audience" requested alongside every service account
	// token. Hydra treats the registered audience as an allowlist, not something it stamps onto
	// the token automatically, so this must be sent explicitly or the token's aud claim comes
	// back empty and the API rejects it. Overridable via service_account_audience /
	// CRUSOE_SERVICE_ACCOUNT_AUDIENCE.
	defaultServiceAccountAudience = "https://api.crusoe.ai"
)

// Config holds options that can be set via ~/.crusoe/config and env variables.
type Config struct {
	ProfileName      string
	AccessKeyID      string `toml:"access_key_id"`
	SecretKey        string `toml:"secret_key"`
	SSHPublicKeyFile string `toml:"ssh_public_key_file"`
	ApiEndpoint      string `toml:"api_endpoint"`
	DefaultProject   string `toml:"default_project"`

	// ServiceAccountClientID and ServiceAccountClientSecret authenticate via OAuth2
	// client_credentials, as an alternative to AccessKeyID/SecretKey. See ResolveAuthMethod.
	ServiceAccountClientID     string `toml:"service_account_client_id"`
	ServiceAccountClientSecret string `toml:"service_account_client_secret"`

	// ServiceAccountTokenURL is the OAuth2 token endpoint for the client_credentials exchange.
	// Defaults to Crusoe's production Hydra endpoint.
	ServiceAccountTokenURL string `toml:"service_account_token_url"`

	// ServiceAccountAudience is the "audience" requested on the token request. Defaults to
	// prod's audience; must match the target environment's auth-gateway.
	ServiceAccountAudience string `toml:"service_account_audience"`
}

// ConfigOptions allows overriding config defaults from the provider block.
// Empty strings are treated as "not specified" and fall through to the next precedence level.
type ConfigOptions struct {
	// Profile specifies which profile to load from ~/.crusoe/config.
	// Precedence: opts.Profile > CRUSOE_PROFILE env > config file "profile" key > "default"
	Profile string

	// Project specifies the default project (name or UUID).
	// Precedence: opts.Project > CRUSOE_DEFAULT_PROJECT env > profile's default_project
	Project string

	// ConfigPath overrides the default ~/.crusoe/config path. Empty uses default.
	ConfigPath string
}

// migrateEndpoint maps legacy API endpoints to the current domain and version.
// Returns the new endpoint if migration is needed, or empty string if no change is required.
func migrateEndpoint(endpoint string) string {
	migrations := map[string]string{
		"https://api.crusoecloud.com/v1alpha5": defaultApiEndpoint,
		"https://api.crusoecloud.com/v1":       defaultApiEndpoint,
	}

	if newEndpoint, ok := migrations[endpoint]; ok {
		return newEndpoint
	}

	return ""
}

// GetConfig populates a config struct based on default values, the user's Crusoe config file, and environment variables.
// This is a convenience wrapper for GetConfigWithOptions with no options.
func GetConfig() (*Config, error) {
	return GetConfigWithOptions(ConfigOptions{})
}

// GetConfigWithOptions populates a config struct based on default values, the user's Crusoe config file,
// provider options, and environment variables. The config file used is ~/.crusoe/config.
//
// The returned *Config is always non-nil, even on error: a non-nil error only means the config
// file couldn't be located or read, and callers should treat it as advisory, not fatal.
//
// Precedence for profile selection (highest to lowest):
//  1. opts.Profile (from provider block)
//  2. CRUSOE_PROFILE environment variable
//  3. "profile" key in config file
//  4. "default"
//
// Precedence for project selection (highest to lowest):
//  1. opts.Project (from provider block)
//  2. CRUSOE_DEFAULT_PROJECT environment variable
//  3. default_project from selected profile
func GetConfigWithOptions(opts ConfigOptions) (*Config, error) {
	config := Config{
		ApiEndpoint:            defaultApiEndpoint,
		ServiceAccountTokenURL: defaultServiceAccountTokenURL,
		ServiceAccountAudience: defaultServiceAccountAudience,
	}

	// A home-dir lookup failure falls through like a missing config file - env vars alone are a
	// valid way to configure the provider. homeDirErr is still returned so the caller can warn.
	var homeDirErr error
	configPath := opts.ConfigPath
	if configPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDirErr = fmt.Errorf("failed to find home dir: %w", err)
		} else {
			configPath = homeDir + configFilePath
		}
	}

	var rawData map[string]interface{}
	// Missing config/invalid config file is valid - credentials can come from env vars
	if homeDirErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", homeDirErr)
		fmt.Fprintf(os.Stderr, "Continuing with environment variables only.\n")
		rawData = make(map[string]interface{})
	} else if _, err := toml.DecodeFile(configPath, &rawData); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Info: config file not found at %s\n", configPath)
			fmt.Fprintf(os.Stderr, "Using environment variables only.\n")
		} else {
			fmt.Fprintf(os.Stderr, "Warning: error reading config file at %s: %v\n", configPath, err)
			fmt.Fprintf(os.Stderr, "Continuing with environment variables only.\n")
		}
		rawData = make(map[string]interface{})
	}

	topLevelProfile := ""
	if profileVal, ok := rawData["profile"]; ok {
		if profileStr, ok := profileVal.(string); ok {
			topLevelProfile = profileStr
		}
	}

	profilesMap := make(map[string]Config)
	for key, val := range rawData {
		valMap, ok := val.(map[string]interface{})
		if !ok {
			continue
		}
		var profileConfig Config
		if accessKey, ok := valMap["access_key_id"].(string); ok {
			profileConfig.AccessKeyID = accessKey
		}
		if secretKey, ok := valMap["secret_key"].(string); ok {
			profileConfig.SecretKey = secretKey
		}
		if sshKey, ok := valMap["ssh_public_key_file"].(string); ok {
			profileConfig.SSHPublicKeyFile = sshKey
		}
		if apiEndpoint, ok := valMap["api_endpoint"].(string); ok {
			profileConfig.ApiEndpoint = apiEndpoint
		}
		if defaultProject, ok := valMap["default_project"].(string); ok {
			profileConfig.DefaultProject = defaultProject
		}
		if clientID, ok := valMap["service_account_client_id"].(string); ok {
			profileConfig.ServiceAccountClientID = clientID
		}
		if clientSecret, ok := valMap["service_account_client_secret"].(string); ok {
			profileConfig.ServiceAccountClientSecret = clientSecret
		}
		if tokenURL, ok := valMap["service_account_token_url"].(string); ok {
			profileConfig.ServiceAccountTokenURL = tokenURL
		}
		if audience, ok := valMap["service_account_audience"].(string); ok {
			profileConfig.ServiceAccountAudience = audience
		}
		profilesMap[key] = profileConfig
	}

	// Profile precedence: provider block > env var > config file > "default"
	profileName := "default"
	if topLevelProfile != "" {
		profileName = topLevelProfile
	}
	if envProfile := os.Getenv("CRUSOE_PROFILE"); envProfile != "" {
		profileName = envProfile
	}
	if opts.Profile != "" {
		profileName = opts.Profile
	}

	config.ProfileName = profileName

	if profileConfig, ok := profilesMap[profileName]; ok {
		if profileConfig.AccessKeyID != "" {
			config.AccessKeyID = profileConfig.AccessKeyID
		}
		if profileConfig.SecretKey != "" {
			config.SecretKey = profileConfig.SecretKey
		}
		if profileConfig.SSHPublicKeyFile != "" {
			config.SSHPublicKeyFile = profileConfig.SSHPublicKeyFile
		}
		if profileConfig.DefaultProject != "" {
			config.DefaultProject = profileConfig.DefaultProject
		}
		if profileConfig.ApiEndpoint != "" {
			config.ApiEndpoint = profileConfig.ApiEndpoint
		}
		if profileConfig.ServiceAccountClientID != "" {
			config.ServiceAccountClientID = profileConfig.ServiceAccountClientID
		}
		if profileConfig.ServiceAccountClientSecret != "" {
			config.ServiceAccountClientSecret = profileConfig.ServiceAccountClientSecret
		}
		if profileConfig.ServiceAccountTokenURL != "" {
			config.ServiceAccountTokenURL = profileConfig.ServiceAccountTokenURL
		}
		if profileConfig.ServiceAccountAudience != "" {
			config.ServiceAccountAudience = profileConfig.ServiceAccountAudience
		}
	}

	// Environment variables for credentials and API endpoint (always override profile)
	if accessKey := os.Getenv("CRUSOE_ACCESS_KEY_ID"); accessKey != "" {
		config.AccessKeyID = accessKey
	}
	if secretKey := os.Getenv("CRUSOE_SECRET_KEY"); secretKey != "" {
		config.SecretKey = secretKey
	}
	if apiEndpoint := os.Getenv("CRUSOE_API_ENDPOINT"); apiEndpoint != "" {
		config.ApiEndpoint = apiEndpoint
	}
	if clientID := os.Getenv("CRUSOE_SERVICE_ACCOUNT_CLIENT_ID"); clientID != "" {
		config.ServiceAccountClientID = clientID
	}
	if clientSecret := os.Getenv("CRUSOE_SERVICE_ACCOUNT_CLIENT_SECRET"); clientSecret != "" {
		config.ServiceAccountClientSecret = clientSecret
	}
	if tokenURL := os.Getenv("CRUSOE_SERVICE_ACCOUNT_TOKEN_URL"); tokenURL != "" {
		config.ServiceAccountTokenURL = tokenURL
	}
	if audience := os.Getenv("CRUSOE_SERVICE_ACCOUNT_AUDIENCE"); audience != "" {
		config.ServiceAccountAudience = audience
	}

	if newEndpoint := migrateEndpoint(config.ApiEndpoint); newEndpoint != "" {
		config.ApiEndpoint = newEndpoint
	}

	// Project precedence: provider block > CRUSOE_DEFAULT_PROJECT env > profile default
	// At this point, config.DefaultProject contains the profile's default_project (if any)
	if defaultProject := os.Getenv("CRUSOE_DEFAULT_PROJECT"); defaultProject != "" && opts.Project == "" {
		config.DefaultProject = defaultProject
	}
	if opts.Project != "" {
		config.DefaultProject = opts.Project
	}

	return &config, homeDirErr
}
