package core

import (
	"os"
	"path/filepath"
	"sync"
)

type SystemSetup struct {
	CertFile, CacheFile string
	Password            string
	mu                  sync.Mutex
}

func initSystem() *SystemSetup {
	if systemOnce == nil {
		systemOnce = &SystemSetup{CertFile: filepath.Join(appOnce.UserDir, "cert.crt"), CacheFile: filepath.Join(appOnce.UserDir, "pass.cache")}
		_ = os.Remove(systemOnce.CacheFile)
	}
	return systemOnce
}
func (s *SystemSetup) initCert() ([]byte, error) {
	if err := atomicWriteFile(s.CertFile, appOnce.PublicCrt, 0644); err != nil {
		return nil, err
	}
	return appOnce.PublicCrt, nil
}
func (s *SystemSetup) SetPassword(password string, _ bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Password = password
	_ = os.Remove(s.CacheFile)
}
func (s *SystemSetup) password() string { s.mu.Lock(); defer s.mu.Unlock(); return s.Password }
