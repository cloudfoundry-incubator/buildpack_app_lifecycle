package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/cloudfoundry-incubator/credhub-cli/credhub"

	yaml "gopkg.in/yaml.v2"
)

type PlatformOptions struct {
	CredhubURI string `json:"credhub_uri"`
}

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "%s: received only %d arguments\n", os.Args[0], len(os.Args)-1)
		fmt.Fprintf(os.Stderr, "Usage: %s <app-directory> <start-command> <metadata> [<platform-options>]", os.Args[0])
		os.Exit(1)
	}

	dir := os.Args[1]
	startCommand := os.Args[2]

	absDir, err := filepath.Abs(dir)
	if err == nil {
		dir = absDir
	}
	os.Setenv("HOME", dir)

	tmpDir, err := filepath.Abs(filepath.Join(dir, "..", "tmp"))
	if err == nil {
		os.Setenv("TMPDIR", tmpDir)
	}

	depsDir, err := filepath.Abs(filepath.Join(dir, "..", "deps"))
	if err == nil {
		os.Setenv("DEPS_DIR", depsDir)
	}

	vcapAppEnv := map[string]interface{}{}
	err = json.Unmarshal([]byte(os.Getenv("VCAP_APPLICATION")), &vcapAppEnv)
	if err == nil {
		vcapAppEnv["host"] = "0.0.0.0"

		vcapAppEnv["instance_id"] = os.Getenv("INSTANCE_GUID")

		port, err := strconv.Atoi(os.Getenv("PORT"))
		if err == nil {
			vcapAppEnv["port"] = port
		}

		index, err := strconv.Atoi(os.Getenv("INSTANCE_INDEX"))
		if err == nil {
			vcapAppEnv["instance_index"] = index
		}

		mungedAppEnv, err := json.Marshal(vcapAppEnv)
		if err == nil {
			os.Setenv("VCAP_APPLICATION", string(mungedAppEnv))
		}
	}

	var command string
	if startCommand != "" {
		command = startCommand
	} else {
		command, err = startCommandFromStagingInfo("staging_info.yml")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid staging info - %s", err)
			os.Exit(1)
		}
	}

	if command == "" {
		fmt.Fprintf(os.Stderr, "%s: no start command specified or detected in droplet", os.Args[0])
		os.Exit(1)
	}

	platformOptions, err := platformOptions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid platform options: %v", err)
		os.Exit(3)
	}
	if platformOptions != nil && platformOptions.CredhubURI != "" {
		if os.Getenv("CF_INSTANCE_CERT") == "" || os.Getenv("CF_INSTANCE_KEY") == "" {
			fmt.Fprintf(os.Stderr, "Missing CF_INSTANCE_CERT and/or CF_INSTANCE_KEY")
			os.Exit(6)
		}
		ch, err := credhub.New(platformOptions.CredhubURI, credhub.ClientCert(os.Getenv("CF_INSTANCE_CERT"), os.Getenv("CF_INSTANCE_KEY")))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to set up credhub client: %v", err)
			os.Exit(4)
		}
		interpolatedServices, err := ch.InterpolateString(os.Getenv("VCAP_SERVICES"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to interpolate credhub references: %v", err)
			os.Exit(5)
		}
		os.Setenv("VCAP_SERVICES", interpolatedServices)
	}

	runtime.GOMAXPROCS(1)
	runProcess(dir, command)
}

func platformOptions() (*PlatformOptions, error) {
	if len(os.Args) > 4 {
		base64PlatformOptions := os.Args[4]
		if base64PlatformOptions == "" {
			return nil, nil
		}
		jsonPlatformOptions, err := base64.StdEncoding.DecodeString(base64PlatformOptions)
		if err != nil {
			return nil, err
		}
		platformOptions := PlatformOptions{}
		err = json.Unmarshal(jsonPlatformOptions, &platformOptions)
		if err != nil {
			return nil, err
		}
		return &platformOptions, nil
	}
	return nil, nil
}

type stagingInfo struct {
	StartCommand string `yaml:"start_command"`
}

func startCommandFromStagingInfo(stagingInfoPath string) (string, error) {
	stagingInfoData, err := ioutil.ReadFile(stagingInfoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	info := stagingInfo{}

	err = yaml.Unmarshal(stagingInfoData, &info)
	if err != nil {
		return "", errors.New("invalid YAML")
	}

	return info.StartCommand, nil
}
