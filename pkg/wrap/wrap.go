package wrap

import (
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xplshn/chains/pkg/chains"
	"github.com/xplshn/chains/pkg/sandbox"
)

// Executes AppImage through bwrap and creates a portable home if one doesn't
// already exist
// Returns error if AppImagePerms.Level < 1
func (ai *AppImage) Sandbox(perms *permissions.AppImagePerms, args []string) error {
	if perms.Level < 1 || perms.Level > 3 {
		return errors.New("permissions level must be 1 - 3")
	}

	if !helpers.DirExists(filepath.Join(xdg.CacheHome, "appimage", ai.md5)) {
		err := os.MkdirAll(filepath.Join(xdg.CacheHome, "appimage", ai.md5), 0744)
		if err != nil {
			return err
		}
	}

	// Tell AppImages not to ask for integration
	if perms.DataDir {
		if !helpers.DirExists(filepath.Join(ai.dataDir, ".local/share/appimagekit")) { // It should always be hardcoded to ~/.local/share/appimagekit. Because the appimage integrators expect this file at this dir
			err := os.MkdirAll(filepath.Join(ai.dataDir, ".local/share/appimagekit"), 0744)
			if err != nil {
				return err
			}
		}

		noIntegrate, _ := os.Create(filepath.Join(ai.dataDir, ".local/share/appimagekit/no_desktopintegration")) // It should always be hardcoded to ~/.local/share/appimagekit. Because the appimage integrators expect this file at this dir
		noIntegrate.Close()
	}

	cmdArgs, err := ai.WrapArgs(perms, args)
	if err != nil {
		return err
	}

	bwrapStr, present := helpers.CommandExists("bwrap")
	if !present {
		return errors.New("failed to find bwrap! unable to sandbox application")
	}

	bwrap := exec.Command(bwrapStr, cmdArgs...)
	bwrap.Stdout = os.Stdout
	bwrap.Stderr = os.Stderr
	bwrap.Stdin = os.Stdin

	return bwrap.Run()
}

// Returns the bwrap arguments to sandbox the AppImage
func (ai AppImage) WrapArgs(perms *permissions.AppImagePerms, args []string) ([]string, error) {
	if !ai.IsMounted() {
		return []string{}, errors.New("AppImage must be mounted before getting its wrap arguments! call *AppImage.Mount() first")
	}

	home, present := unsetHome()
	defer restoreHome(home, present)

	if perms.Level == 0 {
		return args, nil
	}

	cmdArgs := ai.mainWrapArgs(perms)

	// Append console arguments provided by the user
	return append(cmdArgs, args...), nil
}

func (ai *AppImage) mainWrapArgs(perms *permissions.AppImagePerms) []string {
	home, present := unsetHome()
	defer restoreHome(home, present)

	// Basic arguments to be used at all sandboxing levels
	cmdArgs := []string{
		"--setenv", "TMPDIR", "/tmp",
		"--setenv", "HOME", xdg.Home,
		"--setenv", "APPDIR", "/tmp/.mount_" + ai.md5,
		"--setenv", "APPIMAGE", filepath.Join("/app", path.Base(ai.Path)),
		"--setenv", "ARGV0", filepath.Join(path.Base(ai.Path)),
		"--setenv", "XDG_DESKTOP_DIR",     xdg.UserDirs.Desktop,
		"--setenv", "XDG_DOWNLOAD_DIR",    xdg.UserDirs.Download,
		"--setenv", "XDG_DOCUMENTS_DIR",   xdg.UserDirs.Documents,
		"--setenv", "XDG_MUSIC_DIR",       xdg.UserDirs.Music,
		"--setenv", "XDG_PICTURES_DIR",    xdg.UserDirs.Pictures,
		"--setenv", "XDG_VIDEOS_DIR",      xdg.UserDirs.Videos,
		"--setenv", "XDG_TEMPLATES_DIR",   xdg.UserDirs.Templates,
		"--setenv", "XDG_PUBLICSHARE_DIR", xdg.UserDirs.PublicShare,
		"--setenv", "XDG_DATA_HOME",       xdg.DataHome,
		"--setenv", "XDG_CONFIG_HOME",     xdg.ConfigHome,
		"--setenv", "XDG_CACHE_HOME",      xdg.CacheHome,
		"--setenv", "XDG_STATE_HOME",      xdg.StateHome,
		"--setenv", "XDG_RUNTIME_DIR",     xdg.RuntimeDir,
		"--die-with-parent",
		"--perms", "0700",
		"--dir", filepath.Join(xdg.RuntimeDir),
		"--dev", "/dev",
		"--proc", "/proc",
		"--bind", filepath.Join(xdg.CacheHome, "appimage", ai.md5), xdg.CacheHome,
		"--ro-bind-try", ai.resolve("opt"), "/opt",
		"--ro-bind-try", ai.resolve("bin"), "/bin",
		"--ro-bind-try", ai.resolve("sbin"), "/sbin",
		"--ro-bind-try", ai.resolve("lib"), "/lib",
		"--ro-bind-try", ai.resolve("lib32"), "/lib32",
		"--ro-bind-try", ai.resolve("lib64"), "/lib64",
		"--ro-bind-try", ai.resolve("usr/bin"), "/usr/bin",
		"--ro-bind-try", ai.resolve("usr/sbin"), "/usr/sbin",
		"--ro-bind-try", ai.resolve("usr/lib"), "/usr/lib",
		"--ro-bind-try", ai.resolve("usr/lib32"), "/usr/lib32",
		"--ro-bind-try", ai.resolve("usr/lib64"), "/usr/lib64",
		"--dir", "/app",
		"--bind", ai.Path, filepath.Join("/app", path.Base(ai.Path)),
	}

	// Level 1 is minimal sandboxing, grants access to most system files, all devices and only really attempts to isolate home files
	if perms.Level == 1 {
		cmdArgs = append(cmdArgs, []string{
			"--dev-bind", "/dev", "/dev",
			"--ro-bind", "/sys", "/sys",
			"--ro-bind-try", ai.resolve("usr"), "/usr",
			"--ro-bind-try", ai.resolve("etc"), "/etc",
			"--ro-bind-try", ai.resolve("/run/systemd"), "/run/systemd",
			"--ro-bind-try", filepath.Join(xdg.DataHome, "fonts"), filepath.Join(xdg.DataHome, "fonts"),
			"--ro-bind-try", filepath.Join(xdg.DataHome, "themes"), filepath.Join(xdg.DataHome, "themes"),
			"--ro-bind-try", filepath.Join(xdg.DataHome, "icons"), filepath.Join(xdg.DataHome, "icons"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "fontconfig"), filepath.Join(xdg.ConfigHome, "fontconfig"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "gtk-3.0"), filepath.Join(xdg.ConfigHome, "gtk-3.0"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "gtk-4.0"), filepath.Join(xdg.ConfigHome, "gtk-4.0"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "qt5ct"), filepath.Join(xdg.ConfigHome, "qt5ct"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "qt6ct"), filepath.Join(xdg.ConfigHome, "qt6ct"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "Kvantum"), filepath.Join(xdg.ConfigHome, "Kvantum"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "kdeglobals"), filepath.Join(xdg.ConfigHome, "kdeglobals"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "lxde", "lxde.conf"), filepath.Join(xdg.ConfigHome, "lxde", "lxde.conf"),
		}...)
		// Level 2 grants access to fewer system files, and all themes
		// Likely to add more files here for compatability.
		// This should be the standard level for GUI profiles
	} else if perms.Level == 2 {
		cmdArgs = append(cmdArgs, []string{
			"--ro-bind-try", ai.resolve("etc/fonts"), "/etc/fonts",
			"--ro-bind-try", ai.resolve("etc/ld.so.cache"), "/etc/ld.so.cache",
			"--ro-bind-try", ai.resolve("etc/mime.types"), "/etc/mime.types",
			"--ro-bind-try", ai.resolve("etc/xdg"), "/etc/xdg",
			"--ro-bind-try", ai.resolve("usr/share/fontconfig"), "/usr/share/fontconfig",
			"--ro-bind-try", ai.resolve("usr/share/fonts"), "/usr/share/fonts",
			"--ro-bind-try", ai.resolve("usr/share/icons"), "/usr/share/icons",
			"--ro-bind-try", ai.resolve("usr/share/themes"), "/usr/share/themes",
			"--ro-bind-try", ai.resolve("usr/share/applications"), "/usr/share/applications",
			"--ro-bind-try", ai.resolve("usr/share/mime"), "/usr/share/mime",
			"--ro-bind-try", ai.resolve("usr/share/libdrm"), "/usr/share/libdrm",
			"--ro-bind-try", ai.resolve("usr/share/vulkan"), "/usr/share/vulkan",
			"--ro-bind-try", ai.resolve("usr/share/glvnd"), "/usr/share/glvnd",
			"--ro-bind-try", ai.resolve("usr/share/glib-2.0"), "/usr/share/glib-2.0",
			"--ro-bind-try", ai.resolve("usr/share/terminfo"), "/usr/share/terminfo",
			"--ro-bind-try", filepath.Join(xdg.DataHome, "fonts"), filepath.Join(xdg.DataHome, "fonts"),
			"--ro-bind-try", filepath.Join(xdg.DataHome, "themes"), filepath.Join(xdg.DataHome, "themes"),
			"--ro-bind-try", filepath.Join(xdg.DataHome, "icons"), filepath.Join(xdg.DataHome, "icons"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "fontconfig"), filepath.Join(xdg.ConfigHome, "fontconfig"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "gtk-3.0"), filepath.Join(xdg.ConfigHome, "gtk-3.0"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "gtk-4.0"), filepath.Join(xdg.ConfigHome, "gtk-4.0"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "qt5ct"), filepath.Join(xdg.ConfigHome, "qt5ct"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "qt6ct"), filepath.Join(xdg.ConfigHome, "qt6ct"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "Kvantum"), filepath.Join(xdg.ConfigHome, "Kvantum"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "kdeglobals"), filepath.Join(xdg.ConfigHome, "kdeglobals"),
			"--ro-bind-try", filepath.Join(xdg.ConfigHome, "lxde", "lxde.conf"), filepath.Join(xdg.ConfigHome, "lxde", "lxde.conf"),
		}...)
	}

	cmdArgs = append(cmdArgs, parseFiles(perms)...)
	cmdArgs = append(cmdArgs, parseSockets(ai, perms)...)
	cmdArgs = append(cmdArgs, parseDevices(ai, perms)...)
	cmdArgs = append(cmdArgs, "--", "/tmp/.mount_"+ai.md5+"/AppRun")

	if perms.DataDir {
		cmdArgs = append([]string{
			"--bind", ai.dataDir, xdg.Home,
		}, cmdArgs...)
	} else {
		cmdArgs = append([]string{
			"--tmpfs", xdg.Home,
		}, cmdArgs...)
	}

	cmdArgs = append([]string{
		"--bind", ai.tempDir, "/tmp",
		"--bind", ai.mountDir, "/tmp/.mount_" + ai.md5,
	}, cmdArgs...)

	return cmdArgs
}

// Returns the location of the requested directory on the host filesystem with
// symlinks resolved. This should solve systems like GoboLinux, where
// traditionally named directories are symlinks to something unconventional.
func (ai AppImage) resolve(src string) string {
	s, _ := filepath.EvalSymlinks(filepath.Join(ai.rootDir, src))

	if s == "" {
		s = "/" + src
	}

	return s
}

func parseFiles(perms *permissions.AppImagePerms) []string {
	var s []string

	// Convert requested files/ dirs to brap flags
	for _, val := range perms.Files {
		sl := strings.Split(val, ":")
		ex := sl[len(sl)-1]
		dir := strings.Join(sl[:len(sl)-1], ":")

		if ex == "rw" {
			s = append(s, "--bind-try", helpers.ExpandDir(dir), helpers.ExpandGenericDir(dir))
		} else if ex == "ro" {
			s = append(s, "--ro-bind-try", helpers.ExpandDir(dir), helpers.ExpandGenericDir(dir))
		}
	}

	return s
}

// Give all requried flags to add the devices
func parseDevices(ai *AppImage, perms *permissions.AppImagePerms) []string {
	var d []string

	// Convert device perms to bwrap format
	for _, v := range perms.Devices {
		if len(v) < 5 || v[0:5] != "/dev/" {
			v = filepath.Join("/dev", v)
		}

		d = append(d, "--dev-bind-try", v, v)
	}

	// Required files to go along with them
	var devices = map[string][]string{
		"dri": {
			"--ro-bind", "/sys/dev/char", "/sys/dev/char",
			"--ro-bind", "/sys/devices/pci0000:00", "/sys/devices/pci0000:00",
			"--dev-bind-try", "/dev/nvidiactl", "/dev/nvidiactl",
			"--dev-bind-try", "/dev/nvidia0", "/dev/nvidia0",
			"--dev-bind-try", "/dev/nvidia-modeset", "/dev/nvidia-modeset",
			"--ro-bind-try", ai.resolve("usr/share/glvnd"), "/usr/share/glvnd",
		},
		"input": {
			"--ro-bind", "/sys/class/input", "/sys/class/input",
		},
	}

	for device, _ := range devices {
		if _, present := helpers.Contains(perms.Devices, device); present {
			d = append(d, devices[device]...)
		}
	}

	return d
}

func parseSockets(ai *AppImage, perms *permissions.AppImagePerms) []string {
	var s []string
	uid := strconv.Itoa(os.Getuid())

	// These vars will only be used if x11 socket is granted access
	xAuthority := os.Getenv("XAUTHORITY")

	if xAuthority == "" {
		xAuthority = xdg.Home + "/.Xauthority"
	}

	xDisplay := strings.ReplaceAll(os.Getenv("DISPLAY"), ":", "")
	tempDir, present := os.LookupEnv("TMPDIR")
	if !present {
		tempDir = "/tmp"
	}

	// Set if Wayland is running on the host machine
	// Using different Wayland display sessions currently not tested
	wDisplay, waylandEnabled := os.LookupEnv("WAYLAND_DISPLAY")

	// Args if socket is enabled
	var sockets = map[string][]string{
		// Encompasses ALSA, Pulse and pipewire. Easiest for convience, but for
		// more security, specify the specific audio system
		"alsa": {
			"--ro-bind-try", ai.resolve("/usr/share/alsa"), "/usr/share/alsa",
			"--ro-bind-try", ai.resolve("/etc/alsa"), "/etc/alsa",
			"--ro-bind-try", ai.resolve("/etc/group"), "/etc/group",
			"--dev-bind", ai.resolve("/dev/snd"), "/dev/snd",
		},
		"audio": {
			"--ro-bind-try", filepath.Join(xdg.RuntimeDir, "pulse"), "/run/user/" + uid + "/pulse",
			"--ro-bind-try", ai.resolve("/usr/share/alsa"), "/usr/share/alsa",
			"--ro-bind-try", ai.resolve("/usr/share/pulseaudio"), "/usr/share/pulseaudio",
			"--ro-bind-try", ai.resolve("/etc/alsa"), "/etc/alsa",
			"--ro-bind-try", ai.resolve("/etc/group"), "/etc/group",
			"--ro-bind-try", ai.resolve("/etc/pulse"), "/etc/pulse",
			"--dev-bind", ai.resolve("/dev/snd"), "/dev/snd",
		},
		"cgroup": {},
		"dbus": {
			"--ro-bind-try", filepath.Join(xdg.RuntimeDir, "bus"), "/run/user/" + uid + "/bus",
		},
		"ipc": {},
		"network": {
			"--share-net",
			"--ro-bind-try", ai.resolve("/etc/ca-certificates"), "/etc/ca-certificates",
			"--ro-bind-try", ai.resolve("/etc/resolv.conf"), "/etc/resolv.conf",
			"--ro-bind-try", ai.resolve("/etc/ssl"), "/etc/ssl",
			"--ro-bind-try", ai.resolve("/etc/pki"), "/etc/pki",
			"--ro-bind-try", ai.resolve("/usr/share/ca-certificates"), "/usr/share/ca-certificates",
		},
		"pid": {},
		"pipewire": {
			"--ro-bind-try", filepath.Join(xdg.RuntimeDir, "pipewire-0"), "/run/user/" + uid + "/pipewire-0",
		},
		"pulseaudio": {
			"--ro-bind-try", filepath.Join(xdg.RuntimeDir, "pulse"), "/run/user/" + uid + "/pulse",
			// TODO: fix bwrap error when running in level 1
			"--ro-bind-try", ai.resolve("/etc/pulse"), "/etc/pulse",
		},
		"session": {},
		"user":    {},
		"uts":     {},
		"wayland": {
			"--ro-bind-try", filepath.Join(xdg.RuntimeDir, wDisplay), "/run/user/" + uid + "/wayland-0",
			"--ro-bind-try", ai.resolve("/usr/share/X11"), "/usr/share/X11",
			// TODO: Add more enviornment variables for app compatability
			// maybe theres a better way to do this?
			"--setenv", "WAYLAND_DISPLAY", "wayland-0",
			"--setenv", "_JAVA_AWT_WM_NONREPARENTING", "1",
			"--setenv", "MOZ_ENABLE_WAYLAND", "1",
			"--setenv", "XDG_SESSION_TYPE", "wayland",
		},
		// For some reason sometimes it doesn't work when binding X0 to another
		// socket ...but sometimes it does. X11 should be avoided if looking
		// for security anyway, as it easilly allows control of the keyboard
		// and mouse
		"x11": {
			"--ro-bind-try", xAuthority, xdg.Home + "/.Xauthority",
			"--ro-bind-try", tempDir + "/.X11-unix/X" + xDisplay, "/tmp/.X11-unix/X" + xDisplay,
			"--ro-bind-try", ai.resolve("/usr/share/X11"), "/usr/share/X11",
			//"--setenv",      "DISPLAY",         ":"+xDisplay,
			"--setenv", "QT_QPA_PLATFORM", "xcb",
			"--setenv", "XAUTHORITY", xdg.Home + "/.Xauthority",
		},
	}

	// Args to disable sockets if not given
	var unsocks = map[string][]string{
		"alsa":       {},
		"audio":      {},
		"cgroup":     {"--unshare-cgroup-try"},
		"ipc":        {"--unshare-ipc"},
		"network":    {"--unshare-net"},
		"pid":        {"--unshare-pid"},
		"pipewire":   {},
		"pulseaudio": {},
		"session":    {"--new-session"},
		"user":       {"--unshare-user-try"},
		"uts":        {"--unshare-uts"},
		"wayland":    {},
		"x11":        {},
	}

	for socketString, _ := range sockets {
		var present = false
		for _, sock := range perms.Sockets {
			if sock == permissions.Socket(socketString) {
				present = true
			}
		}

		if present {
			// Don't give access to X11 if wayland is running on the machine
			// and the app supports it
			var waylandApp = false
			for _, sock := range perms.Sockets {
				if sock == permissions.Socket("wayland") {
					waylandApp = true
				}
			}

			if waylandEnabled && waylandApp && socketString == "x11" {
				continue
			}

			// If level 1, do not try to share /etc files again
			if socketString == "network" && perms.Level == 1 {
				s = append(s, "--share-net")
				continue
			}

			s = append(s, sockets[socketString]...)
		} else {
			s = append(s, unsocks[socketString]...)
		}
	}

	return s
}

// Unset HOME in case the program using aisap is an AppImage using a portable
// home. This is done because aisap needs access to the acual XDG directories
// to share them. Otherwise, an AppImage requesting `xdg-download` would be
// given the "Download" directory inside of aisap's portable home
func unsetHome() (string, bool) {
	home, present := os.LookupEnv("HOME")

	newHome, _ := helpers.RealHome()

	os.Setenv("HOME", newHome)
	xdg.Reload()

	return home, present
}

// Return the HOME variable to normal
func restoreHome(home string, present bool) {
	if present {
		os.Setenv("HOME", home)
	}

	xdg.Reload()
}
