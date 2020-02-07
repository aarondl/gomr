package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

const (
	gomrFilename = ".gomr"
)

var addCmd = &cobra.Command{
	Use:   "add [flags] <package> [path]",
	Short: "add a replace line to the current module",
	RunE:  addRun,
	Args:  cobra.MinimumNArgs(1),
}

var removeCmd = &cobra.Command{
	Use:   "remove [flags] <package>",
	Short: "Remove a replace from the current module",
	RunE:  removeRun,
	Args:  cobra.ExactArgs(1),
}

var upCmd = &cobra.Command{
	Use:   "up [flags]",
	Short: "Add all stored replace lines to go.mod",
	RunE:  upRun,
}

var downCmd = &cobra.Command{
	Use:   "down [flags]",
	Short: "Remove all stored replace lines from go.mod",
	RunE:  downRun,
}

var rootCmd = &cobra.Command{
	Use:   "gomr [flags] <command>",
	Short: "Manages replaces in Go modules",
}

func main() {
	rootCmd.AddCommand(addCmd, removeCmd, upCmd, downCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
	}
}

func addRun(cmd *cobra.Command, args []string) error {
	moduleName := args[0]
	var absPath string
	if len(args) > 1 {
		absPath = args[1]
	}

	if len(absPath) == 0 {
		// Try to pull this from GOPATH
		absPath = filepath.Join(os.Getenv("GOPATH"), "src", moduleName)
	}

	// If the path doesn't exist on disk bail
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path %s does not exist", absPath)
	} else if err != nil {
		return err
	}

	// Check to see if the path has a go.mod
	addGoMod := false
	if _, err := os.Stat(filepath.Join(absPath, "go.mod")); os.IsNotExist(err) {
		addGoMod = true
	} else if err != nil {
		return err
	}

	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}

	// If we need to add a go.mod do it before we add any replace lines
	if addGoMod {
		if err := gomod(absPath, "init", moduleName); err != nil {
			return errors.Wrapf(err, "failed to go mod init in dir: %s", absPath)
		}
	}

	// Write a replace line into our current module's dir
	err = gomod(modRoot, "edit", fmt.Sprintf("-replace=%s=%s", moduleName, absPath))
	if err != nil {
		return err
	}

	// Finally record it in our magic file
	file, err := os.OpenFile(filepath.Join(modRoot, gomrFilename), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0664)
	if err != nil {
		return errors.Wrapf(err, "failed to open %s file for writing", gomrFilename)
	}

	fmtStr := "%s %s"
	if addGoMod {
		fmtStr = "%s !%s"
	}
	if _, err = fmt.Fprintf(file, fmtStr, moduleName, absPath); err != nil {
		return errors.Wrapf(err, "failed to write to %s", gomrFilename)
	}

	if err = file.Close(); err != nil {
		return errors.Wrapf(err, "failed to close %s file", gomrFilename)
	}

	fmt.Printf("added replace: %s => %s\n", moduleName, absPath)

	return nil
}

func removeRun(cmd *cobra.Command, args []string) error {
	moduleName := args[0]

	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}

	gomrFilePath := filepath.Join(modRoot, gomrFilename)

	replaces, err := readGomrFile(gomrFilePath)
	if err != nil {
		return err
	}

	found := false
	var deleted replace
	for i := 0; i < len(replaces); i++ {
		if strings.ToLower(replaces[i].ModuleName) == strings.ToLower(moduleName) {
			deleted = replaces[i]
			replaces[i] = replaces[len(replaces)-1]
			replaces = replaces[:len(replaces)-1]
			found = true
		}
	}

	if !found {
		fmt.Printf("could not find stored replace for module: %s\n", moduleName)
		return nil
	}

	// First undo the replace we've added
	err = gomod("", "edit", fmt.Sprintf("-dropreplace=%s", moduleName))
	if err != nil {
		return err
	}

	// Then remove the go.mod if we added one
	if deleted.AddGoMod {
		err = os.Remove(filepath.Join(deleted.AbsPath, "go.mod"))
		if err != nil && !os.IsNotExist(err) {
			return errors.Wrap(err, "something went wrong when trying to delete the added go.mod")
		}

		err = os.Remove(filepath.Join(deleted.AbsPath, "go.sum"))
		if err != nil && !os.IsNotExist(err) {
			return errors.Wrap(err, "something went wrong when trying to delete the added go.sum")
		}
	}

	// Persist our new set of replaces
	if err = writeGomrFile(gomrFilePath, replaces); err != nil {
		return errors.Wrap(err, "failed to write gomr file after remove")
	}

	fmt.Printf("deleted replace: %s => %s\n", deleted.ModuleName, deleted.AbsPath)
	return nil
}

func upRun(cmd *cobra.Command, args []string) error {
	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}

	gomrFilePath := filepath.Join(modRoot, gomrFilename)
	replaces, err := readGomrFile(gomrFilePath)
	if err != nil {
		return err
	}

	var replaceArgs []string
	for _, r := range replaces {
		// Add the go.mod if we need it
		if r.AddGoMod {
			if err := gomod(r.AbsPath, "init", r.ModuleName); err != nil {
				return errors.Wrapf(err, "failed to go mod init in dir: %s", r.AbsPath)
			}
		}
		replaceArgs = append(replaceArgs, fmt.Sprintf("-replace=%s=%s", r.ModuleName, r.AbsPath))
	}

	// Add the replace lines to our go.mod
	err = gomod("", append([]string{"edit"}, replaceArgs...)...)
	if err != nil {
		return err
	}

	fmt.Println("replace lines installed")
	return nil
}

func downRun(cmd *cobra.Command, args []string) error {
	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}

	gomrFilePath := filepath.Join(modRoot, gomrFilename)
	replaces, err := readGomrFile(gomrFilePath)
	if err != nil {
		return err
	}

	var replaceArgs []string
	for _, r := range replaces {
		// Add the go.mod if we need it
		if r.AddGoMod {
			err = os.Remove(filepath.Join(r.AbsPath, "go.mod"))
			if err != nil && !os.IsNotExist(err) {
				return errors.Wrap(err, "something went wrong when trying to delete the added go.mod")
			}
		}
		replaceArgs = append(replaceArgs, fmt.Sprintf("-dropreplace=%s", r.ModuleName))
	}

	// Add the replace lines to our go.mod
	err = gomod("", append([]string{"edit"}, replaceArgs...)...)
	if err != nil {
		return err
	}

	fmt.Println("replace lines removed")
	return nil
}

type replace struct {
	ModuleName string
	AbsPath    string
	AddGoMod   bool
}

func readGomrFile(path string) ([]replace, error) {
	gomrFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	var replaces []replace

	scanner := bufio.NewScanner(gomrFile)
	for scanner.Scan() {
		var r replace

		splits := strings.Fields(scanner.Text())

		r.ModuleName = splits[0]
		if strings.HasPrefix(splits[1], "!") {
			r.AbsPath = splits[1][1:]
			r.AddGoMod = true
		} else {
			r.AbsPath = splits[1]
		}

		replaces = append(replaces, r)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return replaces, nil
}

func writeGomrFile(path string, replaces []replace) error {
	f, err := os.Create(path)
	if err != nil {
		return errors.Wrapf(err, "failed to open %s file for writing", gomrFilename)
	}

	for _, r := range replaces {
		absPath := r.AbsPath
		if r.AddGoMod {
			absPath = "!" + absPath
		}
		if _, err = fmt.Fprintln(f, "%s %s", r.ModuleName, absPath); err != nil {
			return err
		}
	}

	if err = f.Close(); err != nil {
		return errors.Wrapf(err, "failed to close %s file write", gomrFilename)
	}

	return nil
}

func gomod(dir string, args ...string) error {
	arguments := append([]string{"mod"}, args...)
	cmd := exec.Command("go", arguments...)
	if len(dir) != 0 {
		cmd.Dir = dir
	}
	b, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", b)
		return err
	}

	return nil
}

// findModuleRoot finds our current module's root by searching for a go.mod
func findModuleRoot() (string, error) {
	d, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if d == "" || d == "/" {
			break
		}

		f, err := os.Stat(filepath.Join(d, "go.mod"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return "", err
		}

		if !f.IsDir() {
			// Successfully stat'd go.mod
			return d, nil
		}
	}

	return "", errors.New("could not find a go.mod in the working directory or it's parents")
}
