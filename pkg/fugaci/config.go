package fugaci

type Config struct {
	NodeName          string `yaml:"nodeName"`
	KubeConfigPath    string `yaml:"kubeConfigPath"`
	LogLevel          string `yaml:"logLevel"`
	CurieBinaryPath   string `yaml:"curieBinaryPath"`
	CurieDataRootPath string `yaml:"curieDataRootPath"`

	TLS struct {
		KeyPath  string `yaml:"keyPath"`
		CertPath string `yaml:"certPath"`
	} `yaml:"tls"`
}
