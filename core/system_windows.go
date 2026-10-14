//go:build windows

package core

import (
	"crypto/sha1"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const proxyRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

type windowsProxyBackup struct {
	Enable      *uint32
	Server      *string
	OwnedServer string
}

func proxyBackupPath() string { return filepath.Join(appOnce.UserDir, "proxy-backup.json") }
func notifyProxyChanged() {
	proc := windows.NewLazySystemDLL("wininet.dll").NewProc("InternetSetOptionW")
	proc.Call(0, 39, 0, 0)
	proc.Call(0, 37, 0, 0)
}
func (s *SystemSetup) setProxy() error {
	if err := s.recoverProxy(); err != nil {
		return err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, proxyRegistryPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	backup := windowsProxyBackup{OwnedServer: "127.0.0.1:" + httpServerOnce.Port}
	value, _, err := key.GetIntegerValue("ProxyEnable")
	if err == nil {
		v := uint32(value)
		backup.Enable = &v
	} else if err != registry.ErrNotExist {
		return err
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err == nil {
		backup.Server = &server
	} else if err != registry.ErrNotExist {
		return err
	}
	data, err := json.Marshal(backup)
	if err != nil {
		return err
	}
	if err := atomicWriteFile(proxyBackupPath(), data, 0600); err != nil {
		return err
	}
	if err := key.SetStringValue("ProxyServer", backup.OwnedServer); err != nil {
		return err
	}
	if err := key.SetDWordValue("ProxyEnable", 1); err != nil {
		_ = s.unsetProxy()
		return err
	}
	notifyProxyChanged()
	return nil
}
func (s *SystemSetup) unsetProxy() error {
	data, err := os.ReadFile(proxyBackupPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var backup windowsProxyBackup
	if err := json.Unmarshal(data, &backup); err != nil {
		return err
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, proxyRegistryPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	current, _, err := key.GetStringValue("ProxyServer")
	if err == registry.ErrNotExist {
		return os.Remove(proxyBackupPath())
	}
	if err != nil {
		return err
	}
	// Another application may have taken over. Do not overwrite its settings.
	if current != backup.OwnedServer {
		return os.Remove(proxyBackupPath())
	}
	if backup.Enable != nil {
		err = key.SetDWordValue("ProxyEnable", *backup.Enable)
	} else {
		err = key.DeleteValue("ProxyEnable")
	}
	if err != nil && err != registry.ErrNotExist {
		return err
	}
	if backup.Server != nil {
		err = key.SetStringValue("ProxyServer", *backup.Server)
	} else {
		err = key.DeleteValue("ProxyServer")
	}
	if err != nil && err != registry.ErrNotExist {
		return err
	}
	notifyProxyChanged()
	return os.Remove(proxyBackupPath())
}
func (s *SystemSetup) recoverProxy() error { return s.unsetProxy() }

func (s *SystemSetup) installCert() (string, error) {
	certData, err := s.initCert()
	if err != nil {
		return "", errors.New("installCert1:" + err.Error())
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		return "", errors.New("Failed to parse certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", errors.New("installCert3:" + err.Error())
	}

	rootStorePtr, err := windows.UTF16PtrFromString("ROOT")
	if err != nil {
		return "", errors.New("installCert4:" + err.Error())
	}

	store, err := windows.CertOpenStore(windows.CERT_STORE_PROV_SYSTEM, 0, 0, windows.CERT_SYSTEM_STORE_LOCAL_MACHINE, uintptr(unsafe.Pointer(rootStorePtr)))
	if err != nil {
		return "", errors.New("installCert5:" + err.Error())
	}
	defer windows.CertCloseStore(store, 0)

	certContext, err := windows.CertCreateCertificateContext(windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING, &cert.Raw[0], uint32(len(cert.Raw)))
	if err != nil {
		return "", errors.New("installCert6:" + err.Error())
	}
	defer windows.CertFreeCertificateContext(certContext)
	err = windows.CertAddCertificateContextToStore(store, certContext, windows.CERT_STORE_ADD_REPLACE_EXISTING, nil)
	if err != nil {
		return "", errors.New("installCert7:" + err.Error())
	}
	return "", nil
}

func (s *SystemSetup) removeLegacyCert() error {
	block, _ := pem.Decode([]byte(legacyCertPEM))
	hash := sha1.Sum(block.Bytes)
	blob := windows.CryptHashBlob{Size: uint32(len(hash)), Data: &hash[0]}
	root, _ := windows.UTF16PtrFromString("ROOT")
	for _, scope := range []uint32{windows.CERT_SYSTEM_STORE_LOCAL_MACHINE, windows.CERT_SYSTEM_STORE_CURRENT_USER} {
		store, err := windows.CertOpenStore(windows.CERT_STORE_PROV_SYSTEM, 0, 0, scope, uintptr(unsafe.Pointer(root)))
		if err != nil {
			return err
		}
		cert, err := windows.CertFindCertificateInStore(store, windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING, 0, windows.CERT_FIND_SHA1_HASH, unsafe.Pointer(&blob), nil)
		if err == nil {
			err = windows.CertDeleteCertificateFromStore(cert)
		} else if errors.Is(err, syscall.Errno(windows.CRYPT_E_NOT_FOUND)) {
			err = nil
		}
		windows.CertCloseStore(store, 0)
		if err != nil {
			return err
		}
	}
	return nil
}
