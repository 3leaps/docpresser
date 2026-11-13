package config

// Config is the process-level configuration. Workload packs, profiles, and
// evidence directories remain explicit command inputs rather than implicit
// global settings.
type Config struct {
	// RunRoot selects the default parent directory for generated run evidence.
	// An empty value requires commands that create evidence to request an output
	// location explicitly.
	RunRoot string `yaml:"run_root"`

	// Source identifies whether a default, file, or environment-selected
	// configuration was loaded. It deliberately does not retain a potentially
	// sensitive path.
	Source string `yaml:"-"`
}
