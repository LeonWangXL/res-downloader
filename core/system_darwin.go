//go:build darwin

package core

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *SystemSetup) runCommand(args []string) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no command provided")
	}

	var cmd *exec.Cmd
	password := s.password()
	if password != "" {
		cmd = exec.Command("sudo", append([]string{"-S"}, args...)...)
		cmd.Stdin = bytes.NewReader([]byte(password + "\n"))
	} else {
		cmd = exec.Command(args[0], args[1:]...)
	}

	output, err := cmd.CombinedOutput()
	return output, err
}

func (s *SystemSetup) getNetworkServices() ([]string, error) {
	output, err := s.runCommand([]string{"networksetup", "-listallnetworkservices"})
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %v", err)
	}

	services := strings.Split(string(output), "\n")
	var activeServices []string
	for _, service := range services {
		service = strings.TrimSpace(service)
		if service == "" || strings.Contains(service, "*") || strings.Contains(service, "Serial Port") {
			continue
		}

		infoOutput, err := s.runCommand([]string{"networksetup", "-getinfo", service})
		if err != nil {
			fmt.Printf("failed to get info for service %s: %v\n", service, err)
			continue
		}

		if strings.Contains(string(infoOutput), "IP address:") {
			activeServices = append(activeServices, service)
		}
	}

	if len(activeServices) == 0 {
		return nil, fmt.Errorf("no active network services found")
	}

	return activeServices, nil
}

type macProxyValue struct {
	Service, Kind, Server, Port string
	Enabled                     bool
}
type macProxyBackup struct {
	Values []macProxyValue
	Port   string
}

func proxyBackupPath() string { return filepath.Join(appOnce.UserDir, "proxy-backup.json") }
func (s *SystemSetup) readMacProxy(service, kind string) (macProxyValue, error) {
	out, err := s.runCommand([]string{"networksetup", "-get" + kind, service})
	if err != nil {
		return macProxyValue{}, fmt.Errorf("read proxy: %s: %w", out, err)
	}
	value := macProxyValue{Service: service, Kind: kind}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		v := strings.TrimSpace(parts[1])
		switch strings.TrimSpace(parts[0]) {
		case "Enabled":
			value.Enabled = v == "Yes"
		case "Server":
			value.Server = v
		case "Port":
			value.Port = v
		case "Authenticated Proxy Enabled":
			if v == "1" || v == "Yes" {
				return value, fmt.Errorf("authenticated system proxy cannot be safely replaced")
			}
		}
	}
	if value.Port == "" {
		return value, fmt.Errorf("invalid proxy settings for %s", service)
	}
	return value, nil
}
func (s *SystemSetup) setProxy() error {
	if err := s.recoverProxy(); err != nil {
		return err
	}
	services, err := s.getNetworkServices()
	if err != nil {
		return err
	}
	backup := macProxyBackup{Port: httpServerOnce.Port}
	for _, service := range services {
		for _, kind := range []string{"webproxy", "securewebproxy"} {
			value, err := s.readMacProxy(service, kind)
			if err != nil {
				return err
			}
			backup.Values = append(backup.Values, value)
		}
	}
	data, _ := json.Marshal(backup)
	if err := atomicWriteFile(proxyBackupPath(), data, 0600); err != nil {
		return err
	}
	for _, value := range backup.Values {
		if out, err := s.runCommand([]string{"networksetup", "-set" + value.Kind, value.Service, "127.0.0.1", backup.Port}); err != nil {
			_ = s.restoreMacProxy(backup, false)
			return fmt.Errorf("set proxy: %s: %w", out, err)
		}
	}
	return nil
}
func (s *SystemSetup) restoreMacProxy(backup macProxyBackup, ownedOnly bool) error {
	for _, value := range backup.Values {
		if ownedOnly {
			current, err := s.readMacProxy(value.Service, value.Kind)
			if err != nil {
				return err
			}
			if current.Server != "127.0.0.1" || current.Port != backup.Port {
				continue
			}
		}
		port, err := strconv.Atoi(value.Port)
		if err != nil {
			return err
		}
		if port > 0 && value.Server != "" {
			if out, err := s.runCommand([]string{"networksetup", "-set" + value.Kind, value.Service, value.Server, value.Port}); err != nil {
				return fmt.Errorf("restore proxy: %s: %w", out, err)
			}
		}
		state := "off"
		if value.Enabled {
			state = "on"
		}
		if out, err := s.runCommand([]string{"networksetup", "-set" + value.Kind + "state", value.Service, state}); err != nil {
			return fmt.Errorf("restore proxy state: %s: %w", out, err)
		}
	}
	return os.Remove(proxyBackupPath())
}
func (s *SystemSetup) unsetProxy() error {
	data, err := os.ReadFile(proxyBackupPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var backup macProxyBackup
	if err := json.Unmarshal(data, &backup); err != nil {
		return err
	}
	return s.restoreMacProxy(backup, true)
}
func (s *SystemSetup) recoverProxy() error { return s.unsetProxy() }

func (s *SystemSetup) installCert() (string, error) {
	_, err := s.initCert()
	if err != nil {
		return "", err
	}
	output, err := s.runCommand([]string{"security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", "/Library/Keychains/System.keychain", s.CertFile})
	if err != nil {
		return string(output), err
	}
	return "", nil
}

func (s *SystemSetup) removeLegacyCert() error {
	block, _ := pem.Decode([]byte(legacyCertPEM))
	sum := sha1.Sum(block.Bytes)
	fingerprint := strings.ToUpper(hex.EncodeToString(sum[:]))
	out, err := s.runCommand([]string{"security", "find-certificate", "-a", "-Z", "/Library/Keychains/System.keychain"})
	if err != nil {
		return fmt.Errorf("check old certificate: %s: %w", out, err)
	}
	if !strings.Contains(strings.ToUpper(string(out)), fingerprint) {
		return nil
	}
	out, err = s.runCommand([]string{"security", "delete-certificate", "-Z", fingerprint, "/Library/Keychains/System.keychain"})
	if err != nil {
		return fmt.Errorf("remove old certificate: %s: %w", out, err)
	}
	return nil
}
