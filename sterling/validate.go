package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// validateForgeModel expands the model without running it. This catches
// reader, syntax, and macro-expansion errors while leaving the currently
// running provider untouched.
func validateForgeModel(opts options) error {
	return runValidation(opts, opts.racket,
		[]string{"-l", "compiler/commands/expand", opts.model})
}

// validateAlloyModel asks the Alloy Analyzer to parse the model and list its
// commands. This catches syntax and type errors before the provider restarts.
func validateAlloyModel(opts options, model string) error {
	// Validate the same staged copy that the provider will run. This matters for
	// models that would otherwise resolve imports against unrelated local files
	// such as alloytools/models/util/integer.als.
	commandModel, cleanup, err := stageAlloyModel(model)
	if err != nil {
		commandModel = model
	} else {
		defer cleanup()
	}
	return runValidation(opts, opts.alloy,
		[]string{"commands", commandModel})
}

func runValidation(opts options, program string, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, program, args...)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("timed out after %ds", int(opts.timeout/time.Second))
		}
		return fmt.Errorf("%v%s", err, formatStderr(stderr.String()))
	}
	return nil
}
