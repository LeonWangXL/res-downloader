package core

import (
	"os"
	"path/filepath"
	"res-downloader/core/shared"
)

type Storage struct {
	fileName string
	def      []byte
}

func NewStorage(filename string, def []byte) *Storage {
	return &Storage{
		fileName: filepath.Join(appOnce.UserDir, filename),
		def:      def,
	}
}

func (l *Storage) Load() ([]byte, error) {
	if !shared.FileExist(l.fileName) {
		err := os.WriteFile(l.fileName, l.def, 0644)
		if err != nil {
			return nil, err
		}
		return l.def, nil
	}
	d, err := os.ReadFile(l.fileName)
	if err != nil {
		return nil, err
	}
	return d, err
}

func (l *Storage) Store(data []byte) error { return atomicWriteFile(l.fileName, data, 0600) }

func atomicWriteFile(filename string, data []byte, mode os.FileMode) (err error) {
	file, err := os.CreateTemp(filepath.Dir(filename), ".resd-store-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer func() { file.Close(); os.Remove(temp) }()
	if err = file.Chmod(mode); err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, filename)
}
