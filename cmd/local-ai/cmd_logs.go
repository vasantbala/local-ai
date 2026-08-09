package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var follow bool
	var lines int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show local-ai's llama-server log",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, paths, err := loadConfig()
			if err != nil {
				return err
			}
			path := filepath.Join(paths.LogsDir, "llama-server.log")

			if err := printTail(path, lines); err != nil {
				return err
			}
			if !follow {
				return nil
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			return followFile(ctx, path)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new log lines as they're written")
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "number of trailing lines to print")
	return cmd
}

func printTail(path string, n int) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("no logs yet at", path)
			return nil
		}
		return err
	}
	defer f.Close()

	// llama-server's own text log is small enough that reading it whole to
	// find the last n lines is simpler than a proper ring buffer, and fast
	// enough in practice.
	var allLines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}
	start := 0
	if len(allLines) > n {
		start = len(allLines) - n
	}
	for _, l := range allLines[start:] {
		fmt.Println(l)
	}
	return nil
}

func followFile(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	reader := bufio.NewReader(f)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		fmt.Print(line)
	}
}
