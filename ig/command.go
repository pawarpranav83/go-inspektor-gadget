package ig

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"syscall"
	"testing"
	"time"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/require"
)

type Command struct {
	// Name of the command to be run, used to give information.
	Name string

	// Cmd is a string of the command which will be run.
	Cmd string

	// ExpectedString contains the exact expected output of the command.
	ExpectedString string

	// ExpectedRegexp contains a regex used to match against the command output.
	ExpectedRegexp string

	// ValidateOutput is a function used to verify the output. It must make the test fail in
	// case of error.
	ValidateOutput func(t *testing.T, output string)

	// Cleanup indicates this command is used to clean resource and should not be
	// skipped even if previous commands failed.
	Cleanup bool

	// StartAndStop indicates this command should first be started then stopped.
	// It corresponds to gadget like execsnoop which wait user to type Ctrl^C.
	StartAndStop bool

	// started indicates this command was started.
	// It is only used by command which have StartAndStop set.
	started bool

	// command is a Cmd object used when we want to start the command, then other
	// do stuff and wait for its completion.
	command *exec.Cmd

	// stdout contains command standard output when started using Startcommand().
	stdout bytes.Buffer

	// stderr contains command standard output when started using Startcommand().
	stderr bytes.Buffer
}

var (
	seed int64 = time.Now().UTC().UnixNano()
	// r    *rand.Rand = rand.New(rand.NewSource(seed))
)

func GetSeed() int64 {
	return seed
}

func (c *Command) IsCleanup() bool {
	return c.Cleanup
}

func (c *Command) IsStartAndStop() bool {
	return c.StartAndStop
}

func (c *Command) Running() bool {
	return c.started
}

// createExecCmd creates an exec.Cmd for the command c.Cmd and stores it in
// Command.command. The exec.Cmd is configured to store the stdout and stderr in
// Command.stdout and Command.stderr so that we can use them on
// Command.verifyOutput().
func (c *Command) createExecCmd() {
	cmd := exec.Command("/bin/sh", "-c", c.Cmd)

	cmd.Stdout = &c.stdout
	cmd.Stderr = &c.stderr

	// To be able to kill the process of /bin/sh and its child (the process of
	// c.Cmd), we need to send the termination signal to their process group ID
	// (PGID). However, child processes get the same PGID as their parents by
	// default, so in order to avoid killing also the integration tests process,
	// we set the fields Setpgid and Pgid of syscall.SysProcAttr before
	// executing /bin/sh. Doing so, the PGID of /bin/sh (and its children)
	// will be set to its process ID, see:
	// https://cs.opensource.google/go/go/+/refs/tags/go1.17.8:src/syscall/exec_linux.go;l=32-34.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}

	c.command = cmd
}

// PrintLogsFn returns a function that print logs in case the test fails.
// func PrintLogsFn(namespaces ...string) func(t *testing.T) {
// 	return func(t *testing.T) {
// 		if !t.Failed() {
// 			return
// 		}

// 		if DefaultTestComponent == InspektorGadgetTestComponent {
// 			t.Logf("Inspektor Gadget pod logs:")
// 			t.Logf(getPodLogs("gadget"))
// 		}

// 		for _, ns := range namespaces {
// 			t.Logf("Logs in namespace %s:", ns)
// 			t.Logf(getPodLogs(ns))
// 		}
// 	}
// }

// verifyOutput verifies if the stdout match with the expected regular expression and the expected
// string. If it doesn't, verifyOutput makes the test fail.
func (c *Command) verifyOutput(t *testing.T) {
	output := c.stdout.String()

	if c.ExpectedRegexp != "" {
		r := regexp.MustCompile(c.ExpectedRegexp)
		if !r.MatchString(output) {
			t.Fatalf("output didn't match the expected regexp: %s", c.ExpectedRegexp)
		}
	}

	if c.ExpectedString != "" {
		require.Equal(t, c.ExpectedString, output, "output didn't match the expected string")
	}

	if c.ValidateOutput != nil {
		c.ValidateOutput(t, output)
	}
}

// verifyOutputWihoutTest verifies the output without using the ValidateOutput function.
func (c *Command) verifyOutputWihoutTest() error {
	output := c.stdout.String()

	if c.ExpectedRegexp != "" {
		r := regexp.MustCompile(c.ExpectedRegexp)
		if !r.MatchString(output) {
			return fmt.Errorf("output didn't match the expected regexp: %s", c.ExpectedRegexp)
		}
	}

	if c.ExpectedString != "" && output != c.ExpectedString {
		return fmt.Errorf("output didn't match the expected string: %s\n%v",
			c.ExpectedString, pretty.Diff(c.ExpectedString, output))
	}

	return nil
}

// kill kills a command by sending SIGKILL because we want to stop the process
// immediatly and avoid that the signal is trapped.
func (c *Command) kill() error {
	const sig syscall.Signal = syscall.SIGKILL

	// No need to kill, command has not been executed yet or it already exited
	if c.command == nil || (c.command.ProcessState != nil && c.command.ProcessState.Exited()) {
		return nil
	}

	// Given that we set Setpgid, here we just need to send the PID of /bin/sh
	// (which is the same PGID) as a negative number to syscall.Kill(). As a
	// result, the signal will be received by all the processes with such PGID,
	// in our case, the process of /bin/sh and c.Cmd.
	err := syscall.Kill(-c.command.Process.Pid, sig)
	if err != nil {
		return err
	}

	// In some cases, we do not have to wait here because the Cmd was executed
	// with Run(), which already waits. On the contrary, in the case it was
	// executed with Start() thus c.started is true, we need to wait indeed.
	if c.started {
		err = c.command.Wait()
		if err == nil {
			return nil
		}

		// Verify if the error is about the signal we just sent. In that case,
		// do not return error, it is what we were expecting.
		var exiterr *exec.ExitError
		if ok := errors.As(err, &exiterr); !ok {
			return err
		}

		waitStatus, ok := exiterr.Sys().(syscall.WaitStatus)
		if !ok {
			return err
		}

		if waitStatus.Signal() != sig {
			return err
		}

		return nil
	}

	return err
}

// RunWithoutTest runs the Command, this is thought to be used in TestMain().
func (c *Command) RunWithoutTest() error {
	c.createExecCmd()

	fmt.Printf("run command(%s):\n%s\n", c.Name, c.Cmd)
	err := c.command.Run()
	fmt.Printf("Command returned(%s):\n%s\n%s\n",
		c.Name, c.stderr.String(), c.stdout.String())

	if err != nil {
		return fmt.Errorf("running command(%s): %w", c.Name, err)
	}

	if err = c.verifyOutputWihoutTest(); err != nil {
		return fmt.Errorf("invalid command output(%s): %w", c.Name, err)
	}

	return nil
}

// StartWithoutTest starts the Command, this is thought to be used in TestMain().
func (c *Command) StartWithoutTest() error {
	if c.started {
		fmt.Printf("Warn(%s): trying to start command but it was already started\n", c.Name)
		return nil
	}

	c.createExecCmd()

	fmt.Printf("Start command(%s): %s\n", c.Name, c.Cmd)
	err := c.command.Start()
	if err != nil {
		return fmt.Errorf("starting command(%s): %w", c.Name, err)
	}

	c.started = true

	return nil
}

// WaitWithoutTest waits for a Command that was started with StartWithoutTest(),
// this is thought to be used in TestMain().
func (c *Command) WaitWithoutTest() error {
	if !c.started {
		fmt.Printf("Warn(%s): trying to wait for a command that has not been started yet\n", c.Name)
		return nil
	}

	fmt.Printf("Wait for command(%s)\n", c.Name)
	err := c.command.Wait()
	fmt.Printf("Command returned(%s):\n%s\n%s\n",
		c.Name, c.stderr.String(), c.stdout.String())

	if err != nil {
		return fmt.Errorf("waiting for command(%s): %w", c.Name, err)
	}

	c.started = false

	return nil
}

// KillWithoutTest kills a Command started with StartWithoutTest()
// or RunWithoutTest() and we do not need to verify its output. This is thought
// to be used in TestMain().
func (c *Command) KillWithoutTest() error {
	fmt.Printf("Kill command(%s)\n", c.Name)

	if err := c.kill(); err != nil {
		return fmt.Errorf("killing command(%s): %w", c.Name, err)
	}

	return nil
}

// Run runs the Command on the given as parameter test.
func (c *Command) Run(t *testing.T) {
	c.createExecCmd()

	t.Logf("Run command(%s):\n%s\n", c.Name, c.Cmd)
	err := c.command.Run()
	t.Logf("Command returned(%s):\n%s\n%s\n",
		c.Name, c.stderr.String(), c.stdout.String())
	require.NoError(t, err, "failed to run command(%s)", c.Name)

	c.verifyOutput(t)
}

// Start starts the Command on the given as parameter test, you need to
// wait it using Stop().
func (c *Command) Start(t *testing.T) {
	if c.started {
		t.Logf("Warn(%s): trying to start command but it was already started\n", c.Name)
		return
	}

	c.createExecCmd()

	t.Logf("Start command(%s): %s\n", c.Name, c.Cmd)
	err := c.command.Start()
	require.NoError(t, err, "failed to start command(%s)", c.Name)

	c.started = true
}

// Stop stops a Command previously started with Start().
// To do so, it Kill() the process corresponding to this Cmd and then wait for
// its termination.
// Cmd output is then checked with regard to ExpectedString and ExpectedRegexp
func (c *Command) Stop(t *testing.T) {
	if !c.started {
		t.Logf("Warn(%s): trying to stop command but it was not started\n", c.Name)
		return
	}

	t.Logf("Stop command(%s)\n", c.Name)
	err := c.kill()
	t.Logf("Command returned(%s):\n%s\n%s\n",
		c.Name, c.stderr.String(), c.stdout.String())
	require.NoError(t, err, "failed to kill command(%s)", c.Name)

	c.verifyOutput(t)

	c.started = false
}
