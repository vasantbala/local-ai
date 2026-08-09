// Package hf resolves and downloads GGUF files from Hugging Face repos,
// using the same "owner/repo[:quant]" addressing llama-server's own
// -hf-repo flag accepts.
package hf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Ref is a parsed "owner/repo[:quant]" model reference.
type Ref struct {
	Owner string
	Repo  string
	Quant string // may be empty
}

// ParseRef parses "owner/repo[:quant]".
func ParseRef(s string) (Ref, error) {
	main, quant, _ := strings.Cut(s, ":")
	parts := strings.Split(main, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Ref{}, fmt.Errorf(`invalid model reference %q, expected "owner/repo" or "owner/repo:quant"`, s)
	}
	return Ref{Owner: parts[0], Repo: parts[1], Quant: quant}, nil
}

func (r Ref) repoID() string { return r.Owner + "/" + r.Repo }

type hfSibling struct {
	RFilename string `json:"rfilename"`
}

type hfModelInfo struct {
	Siblings []hfSibling `json:"siblings"`
}

// isShardedName reports whether filename looks like one part of a
// multi-file GGUF split, e.g. "model-00001-of-00002.gguf".
func isShardedName(filename string) bool {
	return strings.Contains(filename, "-of-") &&
		strings.Contains(strings.ToLower(filename), "-of-0")
}

// Resolve finds the single GGUF file in the repo matching ref.Quant (or the
// repo's only GGUF file if Quant is empty), and returns its filename and
// direct download URL.
//
// v1 scope: single-file GGUF only. If the only matches are shards of a
// multi-file split, Resolve returns an error naming the limitation instead
// of guessing how to assemble them.
func Resolve(ctx context.Context, ref Ref) (filename, downloadURL string, err error) {
	apiURL := fmt.Sprintf("https://huggingface.co/api/models/%s", ref.repoID())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", "", err
	}
	if tok := os.Getenv("HF_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("querying Hugging Face for %s: %w", ref.repoID(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", "", fmt.Errorf("repo %s not found on Hugging Face", ref.repoID())
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("Hugging Face API returned %s for %s", resp.Status, ref.repoID())
	}

	var info hfModelInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", "", fmt.Errorf("decoding Hugging Face response: %w", err)
	}

	var ggufs []string
	for _, s := range info.Siblings {
		if strings.HasSuffix(strings.ToLower(s.RFilename), ".gguf") {
			ggufs = append(ggufs, s.RFilename)
		}
	}
	if len(ggufs) == 0 {
		return "", "", fmt.Errorf("no .gguf files found in %s", ref.repoID())
	}

	var candidates []string
	if ref.Quant == "" {
		candidates = ggufs
	} else {
		q := strings.ToLower(ref.Quant)
		for _, f := range ggufs {
			if strings.Contains(strings.ToLower(f), q) {
				candidates = append(candidates, f)
			}
		}
		if len(candidates) == 0 {
			return "", "", fmt.Errorf("no .gguf file in %s matches quant %q (available: %s)",
				ref.repoID(), ref.Quant, strings.Join(ggufs, ", "))
		}
	}

	if len(candidates) > 1 {
		allSharded := true
		for _, f := range candidates {
			if !isShardedName(f) {
				allSharded = false
				break
			}
		}
		if allSharded {
			return "", "", fmt.Errorf(
				"%s:%s resolves to a multi-file (sharded) GGUF split (%s) — sharded pulls aren't supported yet, pick a single-file quant instead",
				ref.repoID(), ref.Quant, strings.Join(candidates, ", "))
		}
		return "", "", fmt.Errorf(
			"%s:%s is ambiguous, matches multiple files: %s — narrow the quant",
			ref.repoID(), ref.Quant, strings.Join(candidates, ", "))
	}

	filename = candidates[0]
	downloadURL = fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", ref.repoID(), filename)
	return filename, downloadURL, nil
}

// Download fetches url into destPath with resume support, reporting
// progress via onProgress(downloaded, total) as it goes (total may be -1 if
// unknown). It downloads to destPath+".part" and only renames to destPath on
// success, so a directory scanner (llama-server's --models-dir) never sees a
// partial file under the final name.
func Download(ctx context.Context, url, destPath string, onProgress func(downloaded, total int64)) error {
	partPath := destPath + ".part"

	var startOffset int64
	if fi, err := os.Stat(partPath); err == nil {
		startOffset = fi.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if tok := os.Getenv("HF_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if startOffset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startOffset))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()

	flags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case http.StatusOK:
		flags |= os.O_TRUNC
		startOffset = 0
	case http.StatusPartialContent:
		flags |= os.O_APPEND
	case http.StatusRequestedRangeNotSatisfiable:
		// Already fully downloaded from a previous run.
		startOffset = 0
		flags |= os.O_TRUNC
	default:
		return fmt.Errorf("downloading %s: unexpected status %s", url, resp.Status)
	}

	total := resp.ContentLength
	if total >= 0 {
		total += startOffset
	}

	if err := os.MkdirAll(filepath.Dir(partPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(partPath, flags, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", partPath, err)
	}
	defer f.Close()

	downloaded := startOffset
	buf := make([]byte, 1<<20)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return fmt.Errorf("writing %s: %w", partPath, werr)
			}
			downloaded += int64(n)
			if onProgress != nil {
				onProgress(downloaded, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("downloading %s: %w", url, rerr)
		}
	}

	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(partPath, destPath); err != nil {
		return fmt.Errorf("finalizing %s: %w", destPath, err)
	}
	return nil
}
