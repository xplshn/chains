package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/xplshn/chains/pkg/chains"

	"github.com/spf13/pflag"
	"gopkg.in/ini.v1"
)

var (
	ai                     *chains.AppImage
	argv0                  string
	invalidBundle          = errors.New("failed to open bundle")
	invalidIcon            = errors.New("failed to extract icon")
	invalidThumbnail       = errors.New("failed to extract thumbnail preview")
	invalidPerms           = errors.New("failed to get permissions from profile")
	invalidPermLevel       = errors.New("failed to set permissions level (this shouldn't happen)")
	invalidFallbackProfile = errors.New("failed to set fallback profile")
	invalidSocketSet       = errors.New("failed to set socket")
	cantRun                = errors.New("failed to run application")
)

// Command line flags
var (
	help             = pflag.BoolP("help", "h", false, "display this help menu")
	listPerms        = pflag.BoolP("list-perms", "l", false, "print all permissions to be granted to the app")
	verbose          = pflag.BoolP("verbose", "v", false, "make output more verbose")
	level            = flag.Int("level", -1, "change the permissions level")
	rootDir          = flag.String("root-dir", "", "use a different filesystem root for system files")
	dataDir          = flag.String("data-dir", "", "change the AppImage's sandbox home location")
	noDataDir        = flag.Bool("no-data-dir", false, "force AppImage's HOME to be a tmpfs")
	extractIcon      = flag.String("extract-icon", "", "extract the AppImage's icon")
	extractThumbnail = flag.String("extract-thumbnail", "", "extract the AppImage's thumbnail preview")
	profile          = flag.String("profile", "", "use a profile from a desktop entry")
	fallbackProfile  = flag.String("fallback-profile", "", "set profile to fallback on if one isn't found")
	version          = flag.Bool("version", false, "show the version and quit")
	trustOnce        = flag.Bool("trust-once", false, "trust the AppImage for one run")
	trust            = flag.Bool("trust", false, "set whether the AppImage is trusted or not")

	addFiles   arrayFlags
	rmFiles    arrayFlags
	addDevices arrayFlags
	rmDevices  arrayFlags
	addSockets arrayFlags
	rmSockets  arrayFlags
)

// arrayFlags type for multiple string flags
type arrayFlags []string

// Main function
func main() {
	setupSignalHandler()

	flag.Var(&addFiles, "add-file", "give the sandbox access to a filesystem object")
	flag.Var(&rmFiles, "rm-file", "revoke a file from the sandbox")
	flag.Var(&addDevices, "add-device", "add a device to the sandbox")
	flag.Var(&rmDevices, "rm-device", "remove access to a device")
	flag.Var(&addSockets, "add-socket", "allow the sandbox to access another socket")
	flag.Var(&rmSockets, "rm-socket", "disable a socket")

	handleFlags()

	ai, err := chains.NewAppImage(flag.Arg(0))
	if err != nil {
		fatal(invalidBundle, err)
		return
	}
	defer ai.Destroy()

	if *extractIcon != "" {
		if err := extractIconFromAppImage(ai); err != nil {
			fatal(invalidIcon, err)
			return
		}
		return
	}

	if *extractThumbnail != "" {
		if err := extractThumbnailFromAppImage(ai); err != nil {
			fatal(invalidThumbnail, err)
			return
		}
		return
	}

	perms, err := setPermissions(ai)
	if err != nil {
		fatal(invalidPerms, err)
		return
	}

	// List permissions if required
	if *listPerms {
		listAppImagePermissions(ai, perms)
		return
	}

	if err := mountAppImage(ai); err != nil {
		fatal(err, err)
		return
	}

	if err := configureAppImage(ai, perms); err != nil {
		fatal(cantRun, err)
		return
	}

	if err := ai.Sandbox(perms, flag.Args()[1:]); err != nil {
		fmt.Errorf("sandbox error:", err)
		return
	}
}

// Handle interrupt signal
func setupSignalHandler() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		if *verbose {
			fmt.Println("\nQuitting due to interrupt signal!")
		}
		if ai != nil {
			ai.FuserUmount()
		}
	}()
}

// Handle command line flags
func handleFlags() {
	flag.Parse()

	//if *version {
	//	fmt.Println(chains.Version)
	//	os.Exit(0)
	//}

	if *help || len(os.Args) < 2 {
		flag.Usage()
	}
}

// Extract the AppImage's icon
func extractIconFromAppImage(ai *chains.AppImage) error {
	if *verbose {
		fmt.Printf("Extracting icon to %s", *extractIcon)
	}
	f, err := os.Create(*extractIcon)
	if err != nil {
		return err
	}
	defer f.Close()

	// Open the icon file
	iconFile, err := os.Open(ai.Icon)
	if err != nil {
		return err
	}
	defer iconFile.Close()

	// Copy the icon file to the destination
	_, err = io.Copy(f, iconFile)
	return err
}

// Extract the AppImage's thumbnail
func extractThumbnailFromAppImage(ai *chains.AppImage) error {
	if *verbose {
		fmt.Printf("Extracting thumbnail preview to %s\n", *extractThumbnail)
	}
	thumbnail, err := ai.Thumbnail()
	if err != nil {
		return err
	}

	f, err := os.Create(*extractThumbnail)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, thumbnail)
	return err
}

// Set permissions from profile or defaults
func setPermissions(ai *chains.AppImage) (*chains.AppImagePerms, error) {
	perms, err := ai.GetPermissions()
	if err != nil {
		return perms, err
	}

	if *profile != "" {
		f, err := os.Open(*profile)
		if err != nil {
			return perms, err
		}
		defer f.Close()

		perms, err = chains.FromReader(f)
		if err != nil {
			return perms, err
		}
	}

	// Process permission adjustments
	perms.RemoveFiles(rmFiles...)
	perms.AddFiles(addFiles...)

	for _, file := range rmFiles {
		perms.RemoveFiles(file)
	}

	for _, file := range addFiles {
		perms.AddFiles(file)
	}

	// Setting the permissions level if provided
	if *level > -1 && *level <= 3 {
		if err := perms.SetLevel(*level); err != nil {
			return perms, err
		}
	}

	// Fall back to default level if needed
	if perms.Level < 0 || perms.Level > 3 {
		if *fallbackProfile != "" {
			return loadFallbackProfile(perms)
		}
		perms.Level = 3
	}

	return perms, nil
}

// Load permissions from fallback profile
func loadFallbackProfile(perms *chains.AppImagePerms) (*chains.AppImagePerms, error) {
	f, err := ini.LoadSources(ini.LoadOptions{
		IgnoreInlineComment: true,
	}, *fallbackProfile)
	if err != nil {
		return perms, err
	}
	return chains.FromIni(f)
}

// List the AppImage permissions
func listAppImagePermissions(ai *chains.AppImage, perms *chains.AppImagePerms) {
	if *verbose {
		fmt.Printf("Bundle Info:\nName: %s\nVersion: %s\n", ai.Name, ai.Version)
	}
	// List permissions here
	fmt.Println("Permissions:")
	fmt.Println(perms)
}

// Mount the AppImage
func mountAppImage(ai *chains.AppImage) error {
	return ai.Mount()
}

// Configure AppImage before running
func configureAppImage(ai *chains.AppImage, perms *chains.AppImagePerms) error {
	if *rootDir != "" {
		ai.SetRootDir(*rootDir)
	}
	if *dataDir != "" {
		ai.SetDataDir(*dataDir)
	}
	if *noDataDir {
		perms.DataDir = false
	}
	if flagUsed("trust") {
		ai.SetTrusted(*trust)
	}
	if !ai.Trusted() && !*trustOnce {
		return errors.New("bundle isn't marked trusted")
	}
	return nil
}

// Fatal error handler
func fatal(msg error, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", msg, err)
	os.Exit(1)
}

// Check if a flag has been used
func flagUsed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// String representation for arrayFlags
func (i *arrayFlags) String() string {
	return ""
}

// Type representation for arrayFlags
func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}
