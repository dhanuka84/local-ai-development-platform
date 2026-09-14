// sdlc-verifier is the trusted entry point of the isolated evaluator image.
// It receives no gateway, model, forge or deployment credential.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
)

func main() {
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 3*1024*1024+1))
	if err != nil || len(raw) > 3*1024*1024 {
		fmt.Fprintln(os.Stderr, "bounded verifier request required")
		os.Exit(2)
	}
	var in struct {
		Packet workpacket.Packet `json:"packet"`
		Patch  string            `json:"patch"`
	}
	if execution.DecodeProposal(raw, &in) != nil {
		fmt.Fprintln(os.Stderr, "invalid verifier request")
		os.Exit(2)
	}
	// This physical binding is owned by the worker. The accepted base revision,
	// allowed/forbidden paths, checks and limits remain byte-for-byte equivalent.
	// Materialize the read-only snapshot as this unprivileged UID. Git's
	// ownership protection remains enabled; no broad safe.directory exception
	// or host ownership change is needed for the bind-mounted input.
	root, err := os.MkdirTemp("", "sdlc-owned-snapshot-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot prepare evaluator snapshot")
		os.Exit(2)
	}
	defer os.RemoveAll(root)
	if err = copySnapshot("/input/repository", root); err != nil {
		fmt.Fprintln(os.Stderr, "invalid or oversized evaluator snapshot")
		os.Exit(2)
	}
	in.Packet.Workspace = root
	result := workpacket.VerifyPatch(context.Background(), in.Packet, []byte(in.Patch))
	if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
		os.Exit(2)
	}
}

func copySnapshot(source, destination string) error {
	var total int64
	files := 0
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported snapshot entry")
		}
		files++
		total += info.Size()
		if files > 10000 || total > 64*1024*1024 {
			return fmt.Errorf("snapshot budget exceeded")
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm()&0755|0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(output, io.LimitReader(input, 64*1024*1024+1))
		closeErr := output.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}
