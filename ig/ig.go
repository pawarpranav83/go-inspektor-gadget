package ig

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
)

type IG struct {
	path  string
	image string
	v1    int
	v2    int
	v3    int
}

type option func(*IG)

func Path(path string) option {
	return func(ig *IG) {
		ig.path = path
	}
}

func Image(image string) option {
	return func(ig *IG) {
		ig.image = image
	}
}

// Runs "ig version" to get the version string
func getIgVersionString(path string) (string, error) {
	cmd := exec.Command(path, "version")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

// Returns the first three components of the version
// e.g. "v0.26.0" would return (0, 26, 0)
func extractIgVersion(str string) (int, int, int, error) {
	versionMatcher := regexp.MustCompile(`v([0-9]+)\.([0-9]+)\.([0-9]+)`)
	result := versionMatcher.FindStringSubmatch(str)
	if result == nil {
		return 0, 0, 0, fmt.Errorf("no ig version found in string: %s", str)
	}

	v1, err := strconv.Atoi(result[1])
	if err != nil {
		return 0, 0, 0, err
	}

	v2, err := strconv.Atoi(result[2])
	if err != nil {
		return 0, 0, 0, err
	}

	v3, err := strconv.Atoi(result[3])
	if err != nil {
		return 0, 0, 0, err
	}

	return v1, v2, v3, nil
}

// New creates a new IG configured with the options passed as parameters.
// Supported parameters are:
//
//	Image(gadget_image)
//	Path(string)
func New(opts ...option) (*IG, error) {

	ig := &IG{
		path: "",
	}

	for _, opt := range opts {
		opt(ig)
	}

	// if path wasn't preset through New(Path()), autodiscover it
	cmd := ""
	if ig.path == "" {
		cmd = "ig"
	} else {
		cmd = ig.path
	}
	path, err := exec.LookPath(cmd)
	if err != nil {
		return nil, err
	}
	ig.path = path

	vstring, err := getIgVersionString(ig.path)
	if err != nil {
		return nil, fmt.Errorf("could not get ig version: %v", err)
	}
	v1, v2, v3, err := extractIgVersion(vstring)
	if err != nil {
		return nil, fmt.Errorf("failed to extract ig version from [%s]: %v", vstring, err)
	}
	ig.v1 = v1
	ig.v2 = v2
	ig.v3 = v3

	return ig, nil
}

func (ig *IG) Pull(flags ...string) error {
	cmd := append([]string{"image", "pull", ig.image}, flags...)
	if err := ig.runWithOutput(cmd); err != nil {
		return err
	}
	return nil
}

func (ig *IG) Push(flags ...string) error {
	cmd := append([]string{"image", "push", ig.image}, flags...)
	if err := ig.runWithOutput(cmd); err != nil {
		return err
	}
	return nil
}

func (ig *IG) Remove(flags ...string) error {
	cmd := append([]string{"image", "remove", ig.image}, flags...)
	if err := ig.runWithOutput(cmd); err != nil {
		return err
	}
	return nil
}

func (ig *IG) Run(flags ...string) (string, error) {
	var stdout bytes.Buffer

	cmd := append([]string{"run", ig.image}, flags...)
	if err := ig.runWithOutput(cmd); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

// runWithOutput runs an ig command with the given arguments,
// writing any stdout output to the os output
// TODO: replace os with custom
func (ig *IG) runWithOutput(args []string) error {
	cmd := exec.Command(ig.path, args...)
	cmd.Env = append(cmd.Env, "IG_EXPERIMENTAL=true")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		switch e := err.(type) {
		case *exec.Error:
			fmt.Println("failed executing:", err)
		case *exec.ExitError:
			fmt.Println("command exit code =", e.ExitCode())
		default:
			panic(err)
		}
	}

	return nil
}
