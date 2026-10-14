//go:build linux

package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (s *SystemSetup) getLinuxDistro() (string, error) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			return strings.Trim(strings.TrimPrefix(line, "ID="), "\""), nil
		}
	}
	return "", fmt.Errorf("could not determine linux distribution")
}

func (s *SystemSetup) runCommand(args []string, sudo bool) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no command provided")
	}

	var cmd *exec.Cmd
	password := s.password()
	if password != "" && sudo {
		cmd = exec.Command("sudo", append([]string{"-S"}, args...)...)
		cmd.Stdin = bytes.NewReader([]byte(password + "\n"))
	} else {
		cmd = exec.Command(args[0], args[1:]...)
	}

	output, err := cmd.CombinedOutput()
	return output, err
}

type linuxProxyBackup struct {
	Values map[string]string
	Port   string
}

var linuxProxyKeys = [][2]string{{"org.gnome.system.proxy", "mode"}, {"org.gnome.system.proxy.http", "host"}, {"org.gnome.system.proxy.http", "port"}, {"org.gnome.system.proxy.https", "host"}, {"org.gnome.system.proxy.https", "port"}}

func proxyBackupPath() string { return filepath.Join(appOnce.UserDir, "proxy-backup.json") }
func (s *SystemSetup) setProxy() error {
	if err := s.recoverProxy(); err != nil {
		return err
	}
	backup := linuxProxyBackup{Values: map[string]string{}, Port: httpServerOnce.Port}
	for _, key := range linuxProxyKeys {
		out, err := s.runCommand([]string{"gsettings", "get", key[0], key[1]}, false)
		if err != nil {
			return fmt.Errorf("read proxy: %s: %w", out, err)
		}
		backup.Values[key[0]+"/"+key[1]] = strings.TrimSpace(string(out))
	}
	data, _ := json.Marshal(backup)
	if err := atomicWriteFile(proxyBackupPath(), data, 0600); err != nil {
		return err
	}
	values := []string{"'manual'", "'127.0.0.1'", backup.Port, "'127.0.0.1'", backup.Port}
	// Enable manual mode last, after both endpoints are configured.
	for _, i := range []int{1, 2, 3, 4, 0} {
		key := linuxProxyKeys[i]
		if out, err := s.runCommand([]string{"gsettings", "set", key[0], key[1], values[i]}, false); err != nil {
			_ = s.restoreLinuxProxy(backup)
			return fmt.Errorf("set proxy: %s: %w", out, err)
		}
	}
	return nil
}
func (s *SystemSetup) restoreLinuxProxy(backup linuxProxyBackup) error {
	for _, i := range []int{1, 2, 3, 4, 0} {
		key := linuxProxyKeys[i]
		value, ok := backup.Values[key[0]+"/"+key[1]]
		if !ok {
			return fmt.Errorf("invalid proxy backup")
		}
		if out, err := s.runCommand([]string{"gsettings", "set", key[0], key[1], value}, false); err != nil {
			return fmt.Errorf("restore proxy: %s: %w", out, err)
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
	var backup linuxProxyBackup
	if err := json.Unmarshal(data, &backup); err != nil {
		return err
	}
	for _, key := range linuxProxyKeys[1:] {
		out, err := s.runCommand([]string{"gsettings", "get", key[0], key[1]}, false)
		if err != nil {
			return err
		}
		expected := backup.Port
		if key[1] == "host" {
			expected = "'127.0.0.1'"
		}
		if strings.TrimSpace(string(out)) != expected {
			return os.Remove(proxyBackupPath())
		}
	}
	return s.restoreLinuxProxy(backup)
}
func (s *SystemSetup) recoverProxy() error { return s.unsetProxy() }

func (s *SystemSetup) installCert() (string, error) {
	_, err := s.initCert()
	if err != nil {
		return "", err
	}

	distro, err := s.getLinuxDistro()
	if err != nil {
		return "", fmt.Errorf("detect distro failed: %w", err)
	}

	certName := appOnce.AppName + ".crt"
	var certPath string
	var updateCmd = []string{"update-ca-certificates"}

	switch distro {
	case "deepin":
		certDir := "/usr/share/ca-certificates/" + appOnce.AppName
		certPath = certDir + "/" + certName
		if out, err := s.runCommand([]string{"mkdir", "-p", certDir}, true); err != nil {
			return string(out), err
		}
	case "arch":
		certPath = "/etc/ca-certificates/trust-source/anchors/" + certName
		if out, err := s.runCommand([]string{"mkdir", "-p", filepath.Dir(certPath)}, true); err != nil {
			return string(out), err
		}
		updateCmd = []string{"update-ca-trust"}
	default:
		certPath = "/usr/local/share/ca-certificates/" + certName
	}

	if output, err := s.runCommand([]string{"cp", "-f", s.CertFile, certPath}, true); err != nil {
		return string(output), fmt.Errorf("copy certificate: %w", err)
	}
	if distro == "deepin" {
		entry := appOnce.AppName + "/" + certName
		confPath := "/etc/ca-certificates.conf"
		if _, err := s.runCommand([]string{"grep", "-qxF", entry, confPath}, true); err != nil {
			if out, err := s.runCommand([]string{"bash", "-c", fmt.Sprintf("echo '%s' >> %s", entry, confPath)}, true); err != nil {
				return string(out), err
			}
		}
	}
	output, err := s.runCommand(updateCmd, true)
	return string(output), err
}
func (s *SystemSetup) removeLegacyCert() error {
	distro, err := s.getLinuxDistro()
	if err != nil {
		return err
	}
	if distro == "arch" {
		oldPath := "/usr/share/ca-certificates/trust-source/" + appOnce.AppName + ".crt"
		if out, err := s.runCommand([]string{"rm", "-f", oldPath}, true); err != nil {
			return fmt.Errorf("remove old certificate: %s: %w", out, err)
		}
		if out, err := s.runCommand([]string{"update-ca-trust"}, true); err != nil {
			return fmt.Errorf("refresh trust: %s: %w", out, err)
		}
	}
	return nil
}
