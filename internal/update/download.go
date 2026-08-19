package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Download streams a release's asset into destDir, reporting progress and
// verifying the published checksum before handing the file back. A file that
// fails verification is deleted, not returned: a corrupt download must never
// be one code path away from becoming the running executable.
func Download(ctx context.Context, client *http.Client, rel Release, destDir string,
	progress func(done, total int64)) (string, error) {

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}

	wantSum := ""
	if rel.SumURL != "" {
		sum, err := fetchSum(ctx, client, rel.SumURL)
		if err != nil {
			return "", fmt.Errorf("could not read the release checksum: %w", err)
		}
		wantSum = sum
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.AssetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "AgentMux-updater")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download answered %s", resp.Status)
	}
	total := resp.ContentLength
	if total <= 0 {
		total = rel.AssetSize
	}

	path := filepath.Join(destDir, rel.AssetName)
	out, err := os.Create(path)
	if err != nil {
		return "", err
	}

	hash := sha256.New()
	var done int64
	buf := make([]byte, 128*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				out.Close()
				os.Remove(path)
				return "", err
			}
			hash.Write(buf[:n])
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			out.Close()
			os.Remove(path)
			return "", readErr
		}
	}
	if err := out.Close(); err != nil {
		os.Remove(path)
		return "", err
	}

	if wantSum != "" {
		got := hex.EncodeToString(hash.Sum(nil))
		if !strings.EqualFold(got, wantSum) {
			os.Remove(path)
			return "", fmt.Errorf("checksum mismatch: the download does not match what was published")
		}
	}
	return path, nil
}

// fetchSum reads a "<hex>  <filename>" checksum file and returns the hex.
func fetchSum(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "AgentMux-updater")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(body))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", fmt.Errorf("checksum file is not in sha256sum format")
	}
	return fields[0], nil
}
