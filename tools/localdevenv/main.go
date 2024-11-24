package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

func verify() bool {
	checklist := Checklist{}
	checklist.addCheckForEnvVariable(K3STokenEnvVariable)
	checklist.addCheckForEnvVariable(FugaciMacWorkstationIPAddressEnvVariable)
	checklist.addCheckForEnvVariable(FugaciK3SServerIPAddressEnvVariable)
	checklist.addExecutableInstallationCheck("kubectl")
	checklist.addExecutableInstallationCheck("cfssl")
	checklist.addExecutableInstallationCheck("cfssljson")
	checklist.addCheck("docker compose installed", checkDockerInstallation)

	checklist.addCheck(fmt.Sprintf("SSH connectivity to '%s'", MacWorkstationNodeName),
		checkSSHConnectivity(MacWorkstationNodeName))
	checklist.addCheck(fmt.Sprintf("'curie' binary exists on '%s'", MacWorkstationNodeName),
		checkBinaryExistsRemotely(MacWorkstationNodeName))
	checklist.addCheck(fmt.Sprintf("'%s' IP address matches env variable '%s'", MacWorkstationNodeName, FugaciMacWorkstationIPAddressEnvVariable),
		func() error {
			return checkIPAddressBehindSSHMatchesEnvVar(MacWorkstationNodeName, FugaciMacWorkstationIPAddressEnvVariable)
		})
	checklist.addCheck("Kubelet default port 10250 is reachable", func() error {
		return checkPortReachable(os.Getenv(FugaciMacWorkstationIPAddressEnvVariable), "10250")
	})
	return checklist.verify()
}

func provision() {
	provisioner, err := NewProvisioner()
	if err != nil {
		log.Fatalf("Error initializing provisioner: %v", err)
	}
	provisioner.Provision()
}

func main() {
	// Parse flags
	generateReadmeFlag := flag.Bool("generate-readme", false, "Generate README.md for this script")
	flag.Parse()

	if *generateReadmeFlag {
		generateReadme()
		return
	}

	if !verify() {
		return
	}
	provision()
}
