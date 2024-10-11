package chains

import (
	"crypto/md5"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"io"

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
	file       *os.File
	// --- MISC -- //
	WrapArgs   []string  // TODO: Get rid of this
	mainWrapArgs []string
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

// Unmounts an AppImage
func (ai *AppImage) Destroy() error {
	if ai == nil {
		return NilAppImage
	} else if ai.Path == "" {
		return NoPath
	} else if !ai.IsMounted() {
		return NotMounted
	}

	err := unmountDir(ai.mountDir)
	if err != nil {
		return err
	}

	ai.mountDir = ""

	ai.file.Close()

	// Clean up
	err = os.RemoveAll(ai.TempDir())

	ai = nil

	return err
}

// Unmounts a directory (lazily in case the process is finishing up)
func unmountDir(mntPt string) error {
	var umount *exec.Cmd

	if _, err := exec.LookPath("fusermount"); err == nil {
		umount = exec.Command("fusermount", "-uz", mntPt)
	} else {
		umount = exec.Command("umount", "-l", mntPt)
	}

	// Run unmount command, returning the stdout+stderr if fail
	out, err := umount.CombinedOutput()
	if err != nil {
		err = errors.New(string(out))
	}

	return err
}

// Thumbnail returns a reader for the `.DirIcon` file of the AppImage
func (ai *AppImage) Thumbnail() (io.Reader, error) {
    // Use reflection to access the unexported field
    v := reflect.ValueOf(ai).Elem()
    imageTypeField := v.FieldByName("imageType")

    if !imageTypeField.IsValid() {
        return nil, errors.New("imageType field not found")
    }

    // Check the value of the unexported field
    if imageTypeField.Int() == -2 {
        r, err := ExtractResourceReader(ai.Path, "icon/256.png")
        if err == nil {
            return r, nil
        }
    }

    return ai.AI.ExtractFileReader(".DirIcon") // ExtractFileReader tries to get an io.ReadCloser for the file at filepath. Returns an error if the path is pointing to a folder. If the path is pointing to a symlink, it will try to return the file being pointed to, but only if it's within the AppImage.
}
func (ai *AppImage) IsMounted() bool {
	return ai.mountDir != ""
}
func (ai *AppImage) TempDir() string {
	return ai.tempDir
}
