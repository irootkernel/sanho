package config

// SecurityRule defines a single command line pattern to be blocked.
type SecurityRule struct {
	Pattern string `yaml:"pattern"`
	Reason  string `yaml:"reason"`
}

// SecurityConfig represents the structure of the security_rules.yaml file.
type SecurityConfig struct {
	Blacklist []SecurityRule `yaml:"blacklist"`
}
