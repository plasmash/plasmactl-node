package join

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/plasmash/plasmactl-platform/pkg/provisioner"
)

// prompter abstracts interactive confirmation so classifyAndConfirm is testable
// as a pure function. Production wiring uses stdinPrompter; tests inject fakes.
type prompter interface {
	askSimpleYN(message string) (bool, error)
	askTypedYes(message string) (bool, error)
}

// stdinPrompter reads from os.Stdin and writes prompt strings via the provided
// writer (typically os.Stdout for the action).
type stdinPrompter struct {
	out io.Writer
	in  io.Reader
}

func newStdinPrompter(out io.Writer) *stdinPrompter {
	return &stdinPrompter{out: out, in: os.Stdin}
}

func (p *stdinPrompter) askSimpleYN(message string) (bool, error) {
	if _, err := fmt.Fprint(p.out, message+" [y/N]: "); err != nil {
		return false, err
	}
	r := bufio.NewReader(p.in)
	answer, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes", nil
}

func (p *stdinPrompter) askTypedYes(message string) (bool, error) {
	if _, err := fmt.Fprint(p.out, message+`Type "yes" to confirm, anything else to abort: `); err != nil {
		return false, err
	}
	r := bufio.NewReader(p.in)
	answer, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "yes", nil
}

// classifyAndConfirm decides whether to proceed with apply based on the plan
// summary and the user's flags. Returns (proceed, error).
//
// Policy:
//   - Any delete or replace → hard refuse with diagnostic error.
//   - Any destructive update + --auto-approve without --allow-destructive-update → refuse with error.
//   - Any destructive update + --auto-approve + --allow-destructive-update → proceed.
//   - Any destructive update + interactive → typed "yes" prompt.
//   - Otherwise (clean) + --auto-approve → proceed.
//   - Otherwise (clean) + interactive → simple [y/N] prompt.
func classifyAndConfirm(j *Join, s *provisioner.PlanSummary, p prompter) (bool, error) {
	if len(s.Deletes) > 0 || len(s.Replacements) > 0 {
		return false, hardRefuseError(s)
	}
	if len(s.DestructiveUpdates) > 0 {
		if j.AutoApprove && !j.AllowDestructiveUpdate {
			return false, autoApproveDestructiveError(s)
		}
		if j.AutoApprove {
			return true, nil
		}
		return p.askTypedYes(destructiveUpdateMessage(s))
	}
	if j.AutoApprove {
		return true, nil
	}
	return p.askSimpleYN(fmt.Sprintf("This will adopt server(s) into Terraform state for platform %q.\nApply?", j.Platform))
}

// hardRefuseError builds the user-facing error returned when the plan would
// delete or recreate resources (unexpected for an adoption flow).
func hardRefuseError(s *provisioner.PlanSummary) error {
	var b strings.Builder
	b.WriteString("node:join refused to apply this plan.\n\n")
	b.WriteString("Terraform would delete or recreate the following resource(s):\n")
	for _, d := range s.Deletes {
		fmt.Fprintf(&b, "  • %s  (action: delete)\n", d.Address)
	}
	for _, r := range s.Replacements {
		fmt.Fprintf(&b, "  • %s  (action: delete-then-create)\n", r.Address)
	}
	b.WriteString("\nThis is unexpected for an adoption flow. Possible causes:\n")
	b.WriteString("  • --server-id does not match the imported resource\n")
	b.WriteString("  • Stale Terraform state at .plasma/node/provision/<env>/terraform.tfstate\n")
	b.WriteString("  • A bug in the provider template\n\n")
	b.WriteString("If you intend to release these servers from OVH, use `node:destroy` instead.\n")
	b.WriteString("Otherwise: re-check --server-id, then re-run.")
	return errors.New(b.String())
}

// autoApproveDestructiveError is returned when destructive updates appear in
// the plan but --auto-approve was passed without --allow-destructive-update.
func autoApproveDestructiveError(s *provisioner.PlanSummary) error {
	var b strings.Builder
	b.WriteString("node:join refused to apply destructive updates under --auto-approve.\n\n")
	b.WriteString("The plan contains destructive in-place updates. To proceed non-interactively,\n")
	b.WriteString("pass both --auto-approve and --allow-destructive-update.\n\n")
	b.WriteString("Affected resources:\n")
	for _, d := range s.DestructiveUpdates {
		fmt.Fprintf(&b, "  • %s (%s)\n", d.Address, strings.Join(d.ChangedAttrs, ", "))
	}
	// trim the trailing newline so the error doesn't print a stray blank line
	return errors.New(strings.TrimRight(b.String(), "\n"))
}

// destructiveUpdateMessage is the warning printed before the typed-yes prompt.
func destructiveUpdateMessage(s *provisioner.PlanSummary) string {
	var b strings.Builder
	b.WriteString("WARNING: This plan contains destructive in-place updates.\n\n")
	for _, d := range s.DestructiveUpdates {
		fmt.Fprintf(&b, "  • %s\n      attributes: %s\n", d.Address, strings.Join(d.ChangedAttrs, ", "))
	}
	b.WriteString("\nThe OVH provider applies these changes via POST /dedicated/server/<name>/install/start,\n")
	b.WriteString("which reinstalls the operating system and wipes the boot disk.\n\n")
	return b.String()
}
