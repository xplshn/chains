package chains

import (
	"archive/zip"
	"bufio"
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"
)

// --- AppImage detection & Offset calculation logic --- //

// GetOffset takes an AppImage (either ELF or shappimage), returning the offset
// of its SquashFS archive
func GetOffset(src string) (int, error) {
	format, err := GetAppImageType(src)
	if err != nil {
		return -1, err
	}

	if format == -2 {
		return getShappImageSize(src)
	} else if format == 2 {
		return getElfSize(src)
	} else if format == 0 {
		return -1, errors.New("AppImage missing `AI\\0x02` magic at offset 0x08!")
	}

	return -1, errors.New("unsupported AppImage type")
}

// Takes a src file as argument, returning the size of the shImg header and
// an error if fail
func getShappImageSize(src string) (int, error) {
	f, err := os.Open(src)
	defer f.Close()
	if err != nil {
		return -1, err
	}

	_, err = f.Stat()
	if err != nil {
		return -1, err
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if len(scanner.Text()) > 10 && scanner.Text()[0:11] == "sfs_offset=" &&
			len(strings.Split(scanner.Text(), "=")) == 2 {

			offHex := strings.Split(scanner.Text(), "=")[1]
			offHex = strings.ReplaceAll(offHex, "'", "")
			offHex = strings.ReplaceAll(offHex, "\"", "")
			o, err := strconv.Atoi(offHex)

			return int(o), err
		}
	}

	return -1, errors.New("unable to find shappimage offset from `sfs_offset` variable")
}

// Function from <github.com/probonopd/go-appimage/internal/helpers/elfsize.go>
// credit goes to respective author; modified from original
// getElfSize takes a src file as argument, returning its size as an int
// and an error if unsuccessful
func getElfSize(src string) (int, error) {
	f, _ := os.Open(src)
	defer f.Close()
	e, err := elf.NewFile(f)
	if err != nil {
		return -1, err
	}

	// Find offsets based on arch
	sr := io.NewSectionReader(f, 0, 1<<63-1)
	var shoff, shentsize, shnum int

	switch e.Class {
	case elf.ELFCLASS64:
		hdr := new(elf.Header64)

		_, err = sr.Seek(0, 0)
		if err != nil {
			return -1, err
		}
		err = binary.Read(sr, e.ByteOrder, hdr)
		if err != nil {
			return -1, err
		}

		shoff = int(hdr.Shoff)
		shnum = int(hdr.Shnum)
		shentsize = int(hdr.Shentsize)
	case elf.ELFCLASS32:
		hdr := new(elf.Header32)

		_, err = sr.Seek(0, 0)
		if err != nil {
			return -1, err
		}
		err := binary.Read(sr, e.ByteOrder, hdr)
		if err != nil {
			return -1, err
		}

		shoff = int(hdr.Shoff)
		shnum = int(hdr.Shnum)
		shentsize = int(hdr.Shentsize)
	default:
		return 0, nil
	}

	return shoff + (shentsize * shnum), nil
}

// Find the type of AppImage
// Returns strings either `1` for ISO disk image AppImage, `2` for type 2
// SquashFS AppImage, `0` for unknown valid ELF or `-2` for shell script
// SquashFS AppImage (shappimage)
func GetAppImageType(src string) (int, error) {
	f, err := os.Open(src)
	defer f.Close()
	if err != nil {
		return -1, err
	}

	_, err = f.Stat()
	if err != nil {
		return -1, err
	}

	if HasMagic(f, "\x7fELF", 0) {
		if HasMagic(f, "AI\x01", 8) {
			// AppImage type is type 1 (standard)
			return 1, nil
		} else if HasMagic(f, "AI\x02", 8) {
			// AppImage type is type 2 (standard)
			return 2, nil
		}
		// Unknown AppImage, but valid ELF
		return 0, nil
	} else if HasMagic(f, "#!/bin/sh\n#.shImg.#", 0) {
		// AppImage is shappimage (shell script SquashFS implementation)
		return -2, nil
	}

	err = errors.New("unable to get AppImage type")
	return -1, err
}

// Checks the magic of a given file against the byte array provided
// if identical, return true
func HasMagic(r io.ReadSeeker, str string, offset int) bool {
	magic := make([]byte, len(str))

	r.Seek(int64(offset), io.SeekStart)

	_, err := io.ReadFull(r, magic[:])
	if err != nil {
		return false
	}

	for i := 0; i < len(str); i++ {
		if magic[i] != str[i] {
			return false
		}
	}

	return true
}

// --- Update Mechanism Logic --- //

func ReadUpdateInfo(src string) (string, error) {
	format, err := GetAppImageType(src)
	if err != nil {
		return "", err
	}

	if format == 2 || format == 1 {
		return readUpdateInfoFromElf(src)
	} else if format == -2 {
		return readUpdateInfoFromShappimage(src)
	}

	return "", errors.New("AppImage is of unknown type")
}

// Taken and modified from
// <https://github.com/AppImageCrafters/appimage-update/blob/945dfa16017496be7a3f21c827a7ffb11124e548/util/util.go>
func readUpdateInfoFromElf(src string) (string, error) {
	elfFile, err := elf.Open(src)
	if err != nil {
		return "", err
	}

	updInfoSect := elfFile.Section(".upd_info")
	if updInfoSect == nil {
		return "", errors.New("ELF missing .upd_info section")
	}

	sectionData, err := updInfoSect.Data()
	if err != nil {
		return "", errors.New("unable to read update information from section")
	}

	str_end := bytes.Index(sectionData, []byte("\000"))
	if str_end == -1 || str_end == 0 {
		return "", errors.New("no update information found")
	}

	return string(sectionData[:str_end]), nil
}

func readUpdateInfoFromShappimage(src string) (string, error) {
	f, err := ExtractResourceReader(src, "update_info")
	if err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		// Quit on first non-matching line as the update info should only be
		// one line long
		if !strings.Contains(scanner.Text(), " APPIMAGE [update_info]") {
			return scanner.Text(), nil
		}
	}

	return "", errors.New("unable to find update information in shImg")
}

// --- GENERAL --- //

// Converts a multi-item INI value into a slice
// eg: `foo;bar;` becomes []string{ "foo", "bar" }
func SplitKey(str string) []string {
	str = strings.ReplaceAll(str, "；", ";")
	f := func(c rune) bool { return c == ';' }

	return strings.FieldsFunc(str, f)
}

func ExtractResource(aiPath string, src string, dest string) error {
	inF, err := ExtractResourceReader(aiPath, src)
	defer inF.Close()
	if err != nil {
		return err
	}

	outF, err := os.Create(dest)
	defer outF.Close()
	if err != nil {
		return err
	}

	_, err = io.Copy(outF, inF)
	return err
}

func ExtractResourceReader(aiPath string, src string) (io.ReadCloser, error) {
	zr, err := zip.OpenReader(aiPath)
	if err != nil {
		return nil, err
	}

	for _, f := range zr.File {
		if f.Name == filepath.Join(".APPIMAGE_RESOURCES", src) {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}

			return rc, nil
		}
	}

	return nil, errors.New("failed to find `" + src + "` in AppImage resources")
}

// Get the home directory using `/etc/passwd`, discarding the $HOME variable.
func RealHome() (string, error) {
	uid := strconv.Itoa(os.Getuid())

	f, err := os.Open("/etc/passwd")
	defer f.Close()
	if err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		s := strings.Split(scanner.Text(), ":")
		if s[2] == uid {
			return s[5], nil
		}
	}

	return "", errors.New("failed to find home for uid `" + uid + "`!")
}

// Retrieves the directory of the running executable
func GetWorkDir() (string, error) {
	e, err := os.Executable()
	if err != nil {
		return "", err
	}

	return path.Dir(e), nil
}

// Checks if a command exists in the system's PATH or the working directory of the running executable ($0) and returns its full path if found
func CommandExists(str string) (string, bool) {
	wd, err := GetWorkDir()
	if err != nil {
		return "", false
	}

	cmd, err := exec.LookPath(filepath.Join(wd, str))
	if err != nil {
		cmd, err = exec.LookPath(str)
		if err != nil {
			return "", false
		}
	}

	return cmd, true
}

// Returns true if any kind of file (including dirs) exists at `path`
func FileExists(path string) bool {
	_, err := os.Stat(path)

	if err != nil {
		return false
	}

	return true
}

func DirExists(path string) bool {
	info, err := os.Stat(path)

	if err != nil {
		return false
	}

	return info.IsDir()
}

// Takes a full path and prefix, creates a temporary directory and returns its path
func MakeTemp(path string, name string) (string, error) {
	dir := filepath.Clean(filepath.Join(path, name))
	err := os.MkdirAll(dir, 0744)
	return dir, err
}

func RandString(seed int, length int) string {
	chars := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	rand.Seed(int64(seed))
	s := make([]rune, length)
	for i := range s {
		s[i] = chars[rand.Intn(len(chars))]
	}
	return string(s)
}

// --- TODO: Find appropiate namespace --- //

// GetSupportedArchitectures retrieves the architectures supported by the AppImage
func GetSupportedArchitectures(file *os.File, desktop *ini.File) ([]string, error) {
	archKey := desktop.Section("Desktop Entry").Key("X-AppImage-Architecture").Value()
	if len(archKey) > 0 {
		return SplitKey(archKey), nil
	}

	e, err := elf.NewFile(file)
	if err != nil {
		return nil, err
	}

	switch e.Machine {
	case elf.EM_386:
		return []string{"i386"}, nil
	case elf.EM_X86_64:
		return []string{"x86_64"}, nil
	case elf.EM_ARM:
		return []string{"armhf"}, nil
	case elf.EM_AARCH64:
		return []string{"aarch64"}, nil
	default:
		return nil, errors.New("unsupported architecture")
	}
}

// --- Cleanup & IO --- //

func Contains(s []string, str string) (int, bool) {
	for i, val := range s {
		if val == str {
			return i, true
		}
	}

	return -1, false
}

// Checks if an array contains any of the elements from another array
func ContainsAny(s []string, s2 []string) (int, bool) {
	for i := range s2 {
		n, present := Contains(s, s2[i])
		if present {
			return n, true
		}
	}

	return -1, false
}

func CleanFile(str string) string {
	// Get the last 3 chars of the file entry
	var ex string
	if len(str) >= 3 {
		ex = str[len(str)-3:]
	}

	str = ExpandDir(str)

	if ex != ":ro" && ex != ":rw" {
		str = str + ":ro"
	}

	return str
}

func CleanFiles(s []string) []string {
	for i := range s {
		s[i] = CleanFile(s[i])
	}

	return s
}

func CleanDevice(str string) string {
	if len(str) > 5 && str[0:5] == "/dev/" {
		str = strings.Replace(str, "/dev/", "", 1)
	}

	return str
}

func CleanDevices(s []string) []string {
	for i := range s {
		s[i] = CleanDevice(s[i])
	}

	return s
}
