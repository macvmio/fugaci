package cmd

import (
	"fmt"
	"github.com/spf13/viper"
	"os"
)

var flagConfigFile string

func initConfig() {
	if flagConfigFile != "" {
		viper.SetConfigFile(flagConfigFile)
	} else if os.Getenv("FUGACI_CONFIG") != "" {
		viper.SetConfigFile(os.Getenv("FUGACI_CONFIG"))
	} else {
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.fugaci")
		viper.AddConfigPath("$HOME/Library/Application Support/Fugaci")
		viper.AddConfigPath("/etc/fugaci")
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}
	viper.SetEnvPrefix("FUGACI")
	viper.AutomaticEnv()

	err := viper.ReadInConfig()
	if err != nil {
		fmt.Printf("error reading viper config '%v': %v", viper.ConfigFileUsed(), err)
	}
	if viper.GetBool("verbose") {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
		fmt.Println("All settings:", viper.AllSettings())
	}
}
