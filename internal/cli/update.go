package cli

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"

	"gorex/internal/rex"
)

// InstallerScript is the installer embedded in the running application.
var InstallerScript string

func updateCmd(args []string, out, errOut io.Writer) (int, bool) {
	if serverTarget != "" {
		fmt.Fprintln(errOut, "gorex update: updates this installation only")
		return ExitUsage, true
	}
	fs := flagSet("update", errOut)
	version := fs.String("version", "", "GitHub release tag (defaults to the latest stable release)")
	if code, done := parse(fs, args); done {
		return code, true
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return ExitUsage, true
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		fmt.Fprintln(errOut, "gorex update: unsupported operating system")
		return ExitFailed, true
	}
	exe, _ := rex.Executable()
	if exe == "" || InstallerScript == "" {
		fmt.Fprintln(errOut, "gorex update: installer unavailable")
		return ExitFailed, true
	}
	argv := []string{"-s", "--", "--update-from", exe}
	if *version != "" {
		argv = append(argv, "--version", *version)
	}
	cmd := exec.Command("sh", argv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(InstallerScript), out, errOut
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			fmt.Fprintln(errOut, err)
		}
		return ExitFailed, true
	}
	return ExitOK, true
}
