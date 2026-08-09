package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
)

func main() {
	if code := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); code != 0 {
		os.Exit(code)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: workpacket <evaluate|verify> --packet FILE [--patch FILE]")
		return 2
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	packetPath := flags.String("packet", "", "path to a work-packet JSON file")
	patchPath := flags.String("patch", "", "path to a unified Git patch")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if *packetPath == "" {
		fmt.Fprintln(stderr, "--packet is required")
		return 2
	}
	packet, err := loadPacket(*packetPath)
	if err != nil {
		fmt.Fprintln(stderr, "workpacket:", err)
		return 2
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	switch args[0] {
	case "evaluate":
		result := workpacket.Evaluate(packet)
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintln(stderr, "workpacket:", err)
			return 1
		}
		if !result.Allowed {
			return 1
		}
		return 0
	case "verify":
		if *patchPath == "" {
			fmt.Fprintln(stderr, "--patch is required for verify")
			return 2
		}
		patch, err := os.ReadFile(*patchPath)
		if err != nil {
			fmt.Fprintln(stderr, "workpacket: read patch:", err)
			return 2
		}
		result := workpacket.VerifyPatch(ctx, packet, patch)
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintln(stderr, "workpacket:", err)
			return 1
		}
		if !result.Accepted {
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}

func loadPacket(path string) (workpacket.Packet, error) {
	file, err := os.Open(path)
	if err != nil {
		return workpacket.Packet{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var packet workpacket.Packet
	if err := decoder.Decode(&packet); err != nil {
		return workpacket.Packet{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return workpacket.Packet{}, errors.New("packet must contain exactly one JSON object")
	}
	return packet, nil
}
