package chains

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/adrg/xdg"
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
	Offset     int                  // Offset of SquashFS image
	AI         *goappimage.AppImage // Using the go-appimage package
	file       *os.File
	// --- MISC -- //
	WrapArgs     []string // TODO: Get rid of this
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

// Set the directory the sandbox pulls system files from
func (ai *AppImage) SetRootDir(d string) {
	ai.rootDir = d
}

// Set the directory for the sandboxed AppImage's `HOME`
func (ai *AppImage) SetDataDir(d string) {
	ai.dataDir = d
}

// Set the directory for the sandboxed AppImage's `TMPDIR`
func (ai *AppImage) SetTempDir(d string) {
	ai.tempDir = d
}

// mount mounts the requested AppImage `src` to `dest`
// Quick, hacky implementation, ideally this should be redone using the
// squashfuse library
func mount(src string, dest string, offset int) error {
	squashfuse, present := CommandExists("squashfuse")
	if !present {
		return errors.New("failed to find squashfuse binary! cannot mount AppImage")
	}

	// Store the error message in a string
	errBuf := &bytes.Buffer{}

	// Convert the offset to a string and mount using squashfuse
	o := strconv.Itoa(offset)
	mnt := exec.Command(squashfuse, "-o", "offset="+o, src, dest)
	mnt.Stderr = errBuf

	if mnt.Run() != nil {
		return errors.New(errBuf.String())
	}

	return nil
}

// Takes an optional argument to mount at a specific location (failing if it
// doesn't exist or more than one arg given. If none given, automatically
// create a temporary directory and mount to it
func (ai *AppImage) Mount(dest ...string) error {
	// If arg given
	if len(dest) > 1 {
		panic("only one argument allowed with *AppImage.Mount()!")
	} else if len(dest) == 1 {
		if !DirExists(dest[0]) {
			return NoMountPoint
		}

		if !isMountPoint(ai.mountDir) {
			return mount(ai.Path, ai.mountDir, ai.Offset)
		}

		return nil
	}

	var err error

	ai.tempDir, err = MakeTemp(xdg.RuntimeDir+"/aisap/tmp", ai.md5)
	if err != nil {
		return err
	}

	ai.mountDir, err = MakeTemp(xdg.RuntimeDir+"/aisap/mount", ai.md5)
	if err != nil {
		return err
	}

	fmt.Println(ai.mountDir)
	fmt.Println(ai.tempDir)

	// Only mount if no previous instances (launched of the same version) are
	// already mounted there. This is to reuse their libraries, save on RAM and
	// to spam the mount list as little as possible
	if !isMountPoint(ai.mountDir) {
		err = mount(ai.Path, ai.mountDir, ai.Offset)
	}

	return err
}

// Returns true if directory is detected as already being mounted
func isMountPoint(dir string) bool {
	f, _ := os.Open("/proc/self/mountinfo")

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		str := strings.Split(scanner.Text(), " ")[4]
		if str == dir {
			return true
		}
	}

	return false
}

// Returns `true` if the AppImage in question is both executable and has
// its profile copied to the aisap config dir. This is to ensure the
// permissions can't change under the user's feet through an update to the
// AppImage
func (ai *AppImage) Trusted() bool {
	aisapConfig := filepath.Join(xdg.DataHome, "aisap", "profiles")
	filePath := filepath.Join(aisapConfig, ai.Name)

	// If the AppImage permissions exist in aisap's config directory and the
	// AppImage is executable, we consider it trusted
	if FileExists(filePath) {
		info, err := os.Stat(ai.Path)
		if err != nil {
			return false
		}

		return info.Mode()&0100 != 0
	}

	return false
}
