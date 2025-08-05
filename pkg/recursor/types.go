package recursor

// RecurseConfig recursor name resolve config
type RecurseConfig struct {
	Enable      bool     `yaml:"enable"`
	TimeoutSec  int      `yaml:"timeoutSec"`
	NameServers []string `yaml:"name_servers"`
}
