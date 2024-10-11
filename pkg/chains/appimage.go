package chains

import (
	"crypto/md5"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/probonopd/go-appimage/src/goappimage"
	"gopkg.in/ini.v1"
)

type AppImage struct {
	Desktop    *ini.File // INI of internal desktop entry
	Path       string    // Location of AppImage
	Icon       string    // Location of AppImage
	dataDir    string    // The AppImage's `HOME` directory
	rootDir    string    // Can be used to give the AppImage fake system files
	tempDir    string    // The AppImage's `/tmp` directory
	mountDir   string    // The location the AppImage is mounted at
	md5        string    // MD5 of AppImage's URI
	Name       string    // AppImage name from the desktop entry
	Version    string
	UpdateInfo string
	AI         *goappimage.AppImage // Using the go-appimage package
}

// Create a new AppImage object from a path using goappimage
func NewAppImage(src string) (*AppImage, error) {
	ai := &AppImage{Path: src}

	if !FileExists(ai.Path) {
		return nil, errors.New("file not found")
	}

	ai.md5 = CalculateMD5(ai.Path)

	appImage, err := goappimage.NewAppImage(ai.Path)
	if err != nil {
		return nil, err
	}
	ai.AI = appImage

	// Set directories and load desktop entry
	ai.rootDir = "/"
	ai.dataDir = ai.Path + ".home"
	ai.tempDir = filepath.Join(os.TempDir(), "appimage-"+ai.md5)

	// Load desktop entry from the appimage
	desktopReader := appImage.Desktop

	ai.Desktop, err = ini.LoadSources(ini.LoadOptions{
		IgnoreInlineComment: true,
	}, desktopReader)
	if err != nil {
		return nil, err
	}

	ai.Name = ai.Desktop.Section("Desktop Entry").Key("Name").String()
	ai.Version = ai.Desktop.Section("Desktop Entry").Key("X-AppImage-Version").String()

	if ai.Version == "" {
		ai.Version = "1.0"
	}

	ai.UpdateInfo = appImage.UpdateInfo

	return ai, nil
}

// Calculate MD5 hash for a file path
func CalculateMD5(path string) string {
	b := md5.Sum([]byte("file://" + path))
	return fmt.Sprintf("%x", b)
}

// Unmount the AppImage using fusermount -u
func (ai *AppImage) FuserUmount() error {
	cmd := exec.Command("fusermount", "-u", ai.mountDir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to unmount: %v", err)
	}
	return nil
}

// Unmount and destroy the AppImage using fusermount -uz
func (ai *AppImage) FuserDestroy() error {
	cmd := exec.Command("fusermount", "-uz", ai.mountDir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to destroy: %v", err)
	}
	return nil
}
