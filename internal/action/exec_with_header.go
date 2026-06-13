package action

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/clawscli/claws/internal/aws"
	"github.com/clawscli/claws/internal/config"
	"github.com/clawscli/claws/internal/dao"
	apperrors "github.com/clawscli/claws/internal/errors"
	"github.com/clawscli/claws/internal/ui"
)

func setAWSEnv(cmd *exec.Cmd, region string) {
	cfg := config.Global()
	region = cmp.Or(region, cfg.Region())
	cmd.Env = aws.BuildSubprocessEnv(cmd.Env, cfg.Selection(), region)
}

// SimpleExec represents a simple exec command without header.
// Implements tea.ExecCommand interface.
type SimpleExec struct {
	Context    context.Context
	Command    string
	Args       []string
	ActionName string // Action name for read-only allowlist check
	SkipAWSEnv bool   // If true, don't inject AWS env vars (for commands that need to write to ~/.aws)

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// SetStdin sets the stdin for the command
func (e *SimpleExec) SetStdin(r io.Reader) { e.stdin = r }

// SetStdout sets the stdout for the command
func (e *SimpleExec) SetStdout(w io.Writer) { e.stdout = w }

// SetStderr sets the stderr for the command
func (e *SimpleExec) SetStderr(w io.Writer) { e.stderr = w }

// Run executes the command
func (e *SimpleExec) Run() error {
	if config.Global().ReadOnly() && !IsExecAllowedInReadOnly(e.ActionName) {
		return ErrReadOnlyDenied
	}

	cmdCtx := e.Context
	if cmdCtx == nil {
		cmdCtx = context.Background()
	}
	cmd, err := e.command(cmdCtx)
	if err != nil {
		return err
	}
	stdin, stdout, stderr := e.stdio()
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if !e.SkipAWSEnv {
		setAWSEnv(cmd, "")
	}

	return cmd.Run()
}

func (e *SimpleExec) stdio() (io.Reader, io.Writer, io.Writer) {
	return stdio(e.stdin, e.stdout, e.stderr)
}

func (e *SimpleExec) command(ctx context.Context) (*exec.Cmd, error) {
	return buildExecCommand(ctx, e.Command, e.Args, nil)
}

func stdio(stdin io.Reader, stdout, stderr io.Writer) (io.Reader, io.Writer, io.Writer) {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	return stdin, stdout, stderr
}

// ResolveExecutable resolves name to the executable path that will be invoked.
// Absolute or relative paths containing a path separator are returned unchanged.
func ResolveExecutable(name string) (string, error) {
	if name == "" {
		return "", ErrEmptyCommand
	}
	if filepath.IsAbs(name) || strings.ContainsRune(name, os.PathSeparator) {
		return name, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", apperrors.Wrapf(err, "resolve executable %q", name)
	}
	return path, nil
}

// ResolveArgsExecutable returns a copy of args with args[0] resolved to the executable path.
func ResolveArgsExecutable(args []string) ([]string, error) {
	if len(args) == 0 || args[0] == "" {
		return nil, ErrEmptyCommand
	}
	resolved, err := ResolveExecutable(args[0])
	if err != nil {
		return nil, err
	}
	out := append([]string(nil), args...)
	out[0] = resolved
	return out, nil
}

// ExecWithHeader represents an exec command that should run with a fixed header
// Implements tea.ExecCommand interface
type ExecWithHeader struct {
	Context    context.Context
	Command    string
	Args       []string
	ActionName string
	Resource   dao.Resource
	Service    string
	ResType    string
	Region     string
	SkipAWSEnv bool

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// SetStdin sets the stdin for the command
func (e *ExecWithHeader) SetStdin(r io.Reader) {
	e.stdin = r
}

// SetStdout sets the stdout for the command
func (e *ExecWithHeader) SetStdout(w io.Writer) {
	e.stdout = w
}

// SetStderr sets the stderr for the command
func (e *ExecWithHeader) SetStderr(w io.Writer) {
	e.stderr = w
}

// Run executes the command with a fixed header at the top
func (e *ExecWithHeader) Run() error {
	if config.Global().ReadOnly() && !IsExecAllowedInReadOnly(e.ActionName) {
		return ErrReadOnlyDenied
	}
	stdin, stdout, stderr := e.stdio()
	_, height := e.prepareTerminal(stdout)
	cmdCtx := e.context()
	cmd, err := e.command(cmdCtx)
	if err != nil {
		return err
	}
	err = e.runCommand(cmd, stdin, stdout, stderr)
	e.finishTerminal(stdout, height)
	e.showFailure(stdin, stdout, err)

	return err
}

func (e *ExecWithHeader) stdio() (io.Reader, io.Writer, io.Writer) {
	return stdio(e.stdin, e.stdout, e.stderr)
}

func (e *ExecWithHeader) context() context.Context {
	if e.Context != nil {
		return e.Context
	}
	return context.Background()
}

func (e *ExecWithHeader) prepareTerminal(stdout io.Writer) (int, int) {
	width, height := terminalSize(stdout)
	headerLines := e.renderHeader(stdout, width)
	scrollTop := headerLines + 1
	_, _ = fmt.Fprintf(stdout, "\x1b[%d;%dr", scrollTop, height)
	_, _ = fmt.Fprintf(stdout, "\x1b[%d;1H", scrollTop)
	return width, height
}

func terminalSize(stdout io.Writer) (int, int) {
	width, height := 80, 24
	if f, ok := stdout.(*os.File); ok {
		if w, h, err := term.GetSize(int(f.Fd())); err == nil {
			width, height = w, h
		}
	}
	return width, height
}

func (e *ExecWithHeader) renderHeader(stdout io.Writer, width int) int {
	header := e.buildHeader(width)
	headerLines := strings.Count(header, "\n") + 1
	_, _ = fmt.Fprint(stdout, "\x1b[2J\x1b[H")
	_, _ = fmt.Fprint(stdout, header)
	_, _ = fmt.Fprintln(stdout, ui.DimStyle().Render(strings.Repeat("─", width)))
	return headerLines + 1
}

func (e *ExecWithHeader) runCommand(cmd *exec.Cmd, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if !e.SkipAWSEnv {
		setAWSEnv(cmd, e.Region)
	}
	return cmd.Run()
}

func (e *ExecWithHeader) finishTerminal(stdout io.Writer, height int) {
	_, _ = fmt.Fprint(stdout, "\x1b[r")
	_, _ = fmt.Fprintf(stdout, "\x1b[%d;1H", height)
}

func (e *ExecWithHeader) showFailure(stdin io.Reader, stdout io.Writer, err error) {
	if err == nil {
		return
	}
	errorStyle := ui.BoldDangerStyle()
	_, _ = fmt.Fprintln(stdout)
	_, _ = fmt.Fprintln(stdout, errorStyle.Render("Command failed: ")+err.Error())
	_, _ = fmt.Fprintln(stdout)
	_, _ = fmt.Fprint(stdout, "Press Enter to continue...")
	buf := make([]byte, 1)
	if f, ok := stdin.(*os.File); ok {
		_, _ = f.Read(buf)
	}
}

func (e *ExecWithHeader) command(ctx context.Context) (*exec.Cmd, error) {
	return buildExecCommand(ctx, e.Command, e.Args, nil)
}

func (e *ExecWithHeader) buildHeader(_ int) string {
	profileDisplay := config.Global().Selection().DisplayName()
	region := e.Region
	if region == "" {
		region = config.Global().Region()
	}
	accountID := config.Global().AccountID()

	titleStyle := ui.TitleStyle()
	labelStyle := ui.DimStyle()
	valueStyle := ui.TextBrightStyle()
	regionStyle := ui.SectionStyle()

	var lines []string

	title := fmt.Sprintf("%s/%s", e.Service, e.ResType)
	lines = append(lines, titleStyle.Render(title))

	resourceLine := labelStyle.Render("Resource: ") + valueStyle.Render(e.Resource.GetName())
	if id := e.Resource.GetID(); id != e.Resource.GetName() {
		resourceLine += labelStyle.Render(" (") + valueStyle.Render(id) + labelStyle.Render(")")
	}
	lines = append(lines, resourceLine)

	contextParts := []string{
		labelStyle.Render("Profile: ") + valueStyle.Render(profileDisplay),
	}
	if region != "" {
		contextParts = append(contextParts, regionStyle.Render("["+region+"]"))
	}
	if accountID != "" {
		contextParts = append(contextParts, labelStyle.Render("Account: ")+valueStyle.Render(accountID))
	}
	lines = append(lines, strings.Join(contextParts, " "))

	lines = append(lines, ui.DimStyle().Italic(true).Render("Press Ctrl+D or type 'exit' to return to claws"))

	return strings.Join(lines, "\n")
}
