package fugaci

type Config struct {
	NodeName       string
	KubeConfigPath string
	LogLevel       string

	TLS struct {
		KeyPath  string
		CertPath string
	}
}
