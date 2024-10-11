package chains

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/adrg/xdg"
	"gopkg.in/ini.v1"
)

var (
	InvalidSocket = errors.New("socket invalid")
)

type File struct {
	Source   string
	Dest     string
	Writable bool
}

type Socket string

func SocketFromString(socketString string) (Socket, error) {
	socket, present := SocketMap[socketString]

	if !present {
		return socket, InvalidSocket
	}

	return socket, nil
}

const (
	X11        Socket = "x11"
	Alsa       Socket = "alsa"
	Audio      Socket = "audio"
	PulseAudio Socket = "pulseaudio"
	Wayland    Socket = "wayland"
	Dbus       Socket = "dbus"
	Cgroup     Socket = "cgroup"
	Network    Socket = "network"
	Pid        Socket = "pid"
	Pipewire   Socket = "pipewire"
	Session    Socket = "session"
	User       Socket = "user"
	Uts        Socket = "uts"
)

var (
	SocketMap = map[string]Socket{
		"x11":        X11,
		"alsa":       Alsa,
		"audio":      Audio,
		"pulseaudio": PulseAudio,
		"wayland":    Wayland,
		"dbus":       Dbus,
		"cgroup":     Cgroup,
		"network":    Network,
		"pid":        Pid,
		"pipewire":   Pipewire,
		"session":    Session,
		"user":       User,
		"uts":        Uts,
	}
)

type AppImagePerms struct {
	Level   int      `json:"level"`      // How much access to system files
	Files   []string `json:"filesystem"` // Grant permission to access files
	Devices []string `json:"devices"`    // Access device files (eg: dri, input)
	Sockets []Socket `json:"sockets"`    // Use sockets (eg: x11, pulseaudio, network)

	// TODO: rename to PersistentHome or something
	DataDir bool `json:"data_dir"` // Whether or not a data dir should be created (only
	// use if the AppImage saves ZERO data eg: 100% online or a game without
	// save files)

	// Only intended for unmarshalling, should not be used for other purposes
	Names []string `json:"names"`
}

// FromIni attempts to read permissions from a provided *ini.File, if fail, it
// will return an *AppImagePerms with a `Level` value of -1 and and error
func FromIni(e *ini.File) (*AppImagePerms, error) {
	p := &AppImagePerms{}

	// Get permissions from keys
	level := e.Section("X-App Permissions").Key("Level").Value()
	filePerms := e.Section("X-App Permissions").Key("Files").Value()
	devicePerms := e.Section("X-App Permissions").Key("Devices").Value()
	socketPerms := e.Section("X-App Permissions").Key("Sockets").Value()

	// Enable saving to a data dir by default
	if e.Section("X-App Permissions").Key("DataDir").Value() == "false" {
		p.DataDir = false
	} else {
		p.DataDir = true
	}

	l, err := strconv.Atoi(level)
	if err != nil || l < 0 || l > 3 {
		p.Level = -1
		return p, err
	} else {
		p.Level = l
	}

	// Split string into slices and clean up the names
	p.AddFiles(SplitKey(filePerms)...)
	p.AddDevices(SplitKey(devicePerms)...)
	p.AddSockets(SplitKey(socketPerms)...)

	return p, nil
}

// FromSystem attempts to read permissions from a provided desktop entry at
// ~/.local/share/chains/profiles/[ai.Name]
// This should be the preferred way to get permissions and gives maximum power
// to the user (provided they use a tool to easily edit these permissions, which
// I'm also planning on making)
func FromSystem(name string) (*AppImagePerms, error) {
	p := &AppImagePerms{}
	var e string

	fp := filepath.Join(xdg.DataHome, "chains", "profiles", name)
	f, err := os.Open(fp)
	if err != nil {
		return p, err
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		e = e + strings.ReplaceAll(scanner.Text(), ";", "；") + "\n"
	}

	entry, err := ini.Load([]byte(e))
	if err != nil {
		return p, err
	}

	p, err = FromIni(entry)

	return p, err
}

func FromReader(r io.Reader) (*AppImagePerms, error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	b = bytes.ReplaceAll(b, []byte(";"), []byte("；"))

	e, err := ini.Load(b)
	if err != nil {
		return nil, err
	}

	return FromIni(e)
}

func (p *AppImagePerms) AddFiles(s ...string) {
	// Remove previous files of the same name if they exist
	p.RemoveFiles(s...)

	p.Files = append(p.Files, CleanFiles(s)...)
}

func (p *AppImagePerms) AddDevices(s ...string) {
	p.RemoveDevices(s...)

	p.Devices = append(p.Devices, CleanDevices(s)...)
}

func (p *AppImagePerms) AddSockets(socketStrings ...string) error {
	if len(socketStrings) == 0 {
		return nil
	}

	p.RemoveSockets(socketStrings...)

	for i := range socketStrings {
		socket, err := SocketFromString(socketStrings[i])

		if err != nil {
			return err
		}

		p.Sockets = append(p.Sockets, socket)
	}

	return nil
}

func (p *AppImagePerms) removeFile(str string) {
	// Done this way to ensure there is an `extension` eg: `:ro` on the string,
	// it will then be used to detect if that file already exists
	str = CleanFiles([]string{str})[0]
	s := strings.Split(str, ":")
	str = strings.Join(s[:len(s)-1], ":")

	if i, present := ContainsAny(p.Files,
		[]string{str + ":ro", str + ":rw"}); present {
		p.Files = append(p.Files[:i], p.Files[i+1:]...)
	}
}

func (p *AppImagePerms) RemoveFiles(s ...string) {
	for i := range s {
		p.removeFile(s[i])
	}
}

func (p *AppImagePerms) removeDevice(str string) {
	if i, present := Contains(p.Devices, str); present {
		p.Devices = append(p.Devices[:i], p.Devices[i+1:]...)
	}
}

func (p *AppImagePerms) RemoveDevices(s ...string) {
	for i := range s {
		p.removeDevice(s[i])
	}
}

// TODO: switch to Socket type
func (p *AppImagePerms) removeSocket(str string) {
	for i, socket := range p.Sockets {
		if str == string(socket) {
			p.Sockets = append(p.Sockets[:i], p.Sockets[i+1:]...)
		}
	}
}

func (p *AppImagePerms) RemoveSockets(s ...string) {
	for i := range s {
		p.removeSocket(s[i])
	}
}

// Set sandbox base permission level
func (p *AppImagePerms) SetLevel(l int) error {
	if l < 0 || l > 3 {
		return errors.New("permissions level must be int from 0-3")
	}

	p.Level = l

	return nil
}

// Set the trusted status
func (ai *AppImage) SetTrusted(trusted bool) error {
	configPath := filepath.Join(xdg.DataHome, "chains", "profiles", ai.Name)

	if trusted {
		if !FileExists(configPath) {
			err := os.MkdirAll(filepath.Dir(configPath), 0744)
			if err != nil {
				return err
			}

			info, err := os.Stat(ai.Path)
			if err != nil {
				return err
			}

			err = os.Chmod(ai.Path, info.Mode()|0100)
			if err != nil {
				return err
			}
		} else {
			return errors.New("entry already exists in chains config dir")
		}
	} else {
		os.Remove(configPath)
	}

	return nil
}

// IsTrusted checks if the AppImage is trusted by verifying its permissions
func IsTrusted(name, path string) bool {
	configPath := filepath.Join(xdg.DataHome, "chains", "profiles", name)

	if FileExists(configPath) {
		info, err := os.Stat(path)
		if err == nil && info.Mode()&0100 != 0 {
			return true
		}
	}
	return false
}

func SetTrusted(name, path string, ai *AppImage, trusted bool) error {
    configPath := filepath.Join(xdg.DataHome, "chains", "profiles", name)

    if trusted {
        if !DirExists(filepath.Dir(configPath)) {
            os.MkdirAll(filepath.Dir(configPath), 0744)
        }

        info, err := os.Stat(path)
        if err != nil {
            return err
        }
        os.Chmod(path, info.Mode()|0100)

        if FileExists(configPath) {
            return errors.New("entry already exists in chains config dir")
        }

        desktopFile := ai.Desktop
        permFile, err := os.Create(configPath)
        if err != nil {
            return err
        }
        defer permFile.Close()

        var buf bytes.Buffer
        if _, err := desktopFile.WriteTo(&buf); err != nil {
            return err
        }
        _, err = io.Copy(permFile, &buf)
        return err
    } else {
        return os.Remove(configPath)
    }
}

// GetPermissions retrieves the permissions of the AppImage
// Retrieve permissions from the AppImage in the following order:
//
//	1: User-configured settings in ~/.local/share/chains/profiles/[ai.Name]
//	2: chains internal permissions library
//	3: Permissions defined in the AppImage's desktop file
func (ai AppImage) GetPermissions() (*AppImagePerms, error) {
	var perms *AppImagePerms
	var err error

	// If PREFER_chains_PROFILE is set, attempt to use it over the AppImage's
	// suggested permissions. If no profile exists in chains, fall back on saved
	// permissions in chains, and then finally the AppImage's internal desktop
	// entry
	// Typically this should be unset unless testing a custom profile against
	// chains's
	if _, present := os.LookupEnv("PREFER_CHAINS_PROFILE"); present {
		perms, err = FromName(ai.Name)

		if err != nil {
			perms, err = FromSystem(ai.Name)
		}
	} else {
		perms, err = FromSystem(ai.Name)

		if err != nil {
			perms, err = FromName(ai.Name)
		}
	}

	// Fall back to permissions inside AppImage if all else fails
	if err != nil {
		return FromIni(ai.Desktop)
	}

	return perms, nil
}
